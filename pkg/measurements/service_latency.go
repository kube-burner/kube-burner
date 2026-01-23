// Copyright 2023 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package measurements

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	mutil "github.com/kube-burner/kube-burner/v2/pkg/measurements/util"
	kutil "github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	lcorev1 "k8s.io/client-go/listers/core/v1"
	ldiscoveryv1 "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	svcLatencyMeasurement          = "svcLatencyMeasurement"
	svcLatencyQuantilesMeasurement = "svcLatencyQuantilesMeasurement"
)

var supportedServiceLatencyJobTypes = map[config.JobType]struct{}{
	config.CreationJob: {},
	config.PatchJob:    {},
}

type serviceLatency struct {
	BaseMeasurement
	stopInformerCh    chan struct{}
	epsLister         ldiscoveryv1.EndpointSliceLister
	svcLister         lcorev1.ServiceLister
	svcLatencyChecker *mutil.SvcLatencyChecker
}

type svcMetric struct {
	Timestamp         time.Time          `json:"timestamp"`
	IPAssignedLatency time.Duration      `json:"ipAssigned,omitempty"`
	ReadyLatency      time.Duration      `json:"ready"`
	MetricName        string             `json:"metricName"`
	UUID              string             `json:"uuid"`
	Namespace         string             `json:"namespace"`
	Name              string             `json:"service"`
	ServiceType       corev1.ServiceType `json:"type"`
	JobName           string             `json:"jobName,omitempty"`
	Metadata          any                `json:"metadata,omitempty"`
}

type serviceLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newServiceLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any, labelSelector string) (MeasurementFactory, error) {
	if measurement.ServiceTimeout == 0 {
		return nil, fmt.Errorf("svcTimeout cannot be 0")
	}

	return serviceLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata, labelSelector),
	}, nil
}

func (slmf serviceLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &serviceLatency{
		BaseMeasurement: slmf.NewBaseLatency(jobConfig, clientSet, restConfig, svcLatencyMeasurement, svcLatencyQuantilesMeasurement, embedCfg),
	}
}

func (s *serviceLatency) handleCreateSvc(obj any) {
	svc, err := kutil.ConvertAnyToTyped[corev1.Service](obj)
	if err != nil {
		log.Errorf("failed to convert to Service: %v", err)
		return
	}
	if annotation, ok := svc.Annotations[config.KubeBurnerLabelServiceLatency]; ok {
		if annotation == "false" {
			log.Debugf("Annotation found, discarding service %v/%v", svc.Namespace, svc.Name)
		}
	}
	log.Debugf("Handling service: %v/%v", svc.Namespace, svc.Name)
	go func(svc *corev1.Service) {
		var ips []string
		var port int32
		var ipAssignedLatency time.Duration
		now := time.Now()
		// If service is loadbalancer first wait for the IP assignment
		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			if err := s.waitForIngress(svc); err != nil {
				log.Fatal(err)
			}
			ipAssignedLatency = time.Since(now)
		}
		if err := s.waitForEndpointSlices(svc); err != nil {
			log.Fatal(err)
		}
		// This conditionals prevents a nil pointer dereference panic that can happen when the
		// measurement is stopped by end of job J, and then the benchmark continues with job J+1,
		// so this goroutine can still be runnning in the background as it performs polling
		if s.epsLister == nil {
			return
		}
		endpointsReadyTs := time.Now().UTC()
		log.Debugf("EndpointSlices for service %v/%v ready", svc.Namespace, svc.Name)
		for _, specPort := range svc.Spec.Ports {
			if specPort.Protocol == corev1.ProtocolTCP { // Support TCP protocol
				switch svc.Spec.Type {
				case corev1.ServiceTypeClusterIP:
					ips = svc.Spec.ClusterIPs
					port = specPort.Port
				case corev1.ServiceTypeNodePort:
					ips = []string{s.svcLatencyChecker.Pod.Status.HostIP}
					port = specPort.NodePort
				case corev1.ServiceTypeLoadBalancer:
					for _, ingress := range svc.Status.LoadBalancer.Ingress {
						if ingress.IP != "" {
							ips = append(ips, ingress.Hostname)
						} else {
							ips = append(ips, ingress.IP)
						}
					}
					port = specPort.Port
				default:
					log.Warnf("Service type %v not supported, skipping", svc.Spec.Type)
					return
				}
				for _, ip := range ips {
					err = s.svcLatencyChecker.Ping(ip, port, s.Config.ServiceTimeout)
					if err != nil {
						log.Error(err)
						return
					}
				}
			}
		}
		svcLatency := time.Since(endpointsReadyTs)
		log.Debugf("Service %v/%v latency was: %vms", svc.Namespace, svc.Name, svcLatency.Milliseconds())
		s.Metrics.Store(string(svc.UID), svcMetric{
			Name:              svc.Name,
			Namespace:         svc.Namespace,
			Timestamp:         svc.CreationTimestamp.UTC(),
			MetricName:        svcLatencyMeasurement,
			ServiceType:       svc.Spec.Type,
			ReadyLatency:      svcLatency,
			UUID:              s.Uuid,
			IPAssignedLatency: ipAssignedLatency,
			JobName:           s.JobConfig.Name,
			Metadata:          s.Metadata,
		})
	}(svc)
}

// start service latency measurement
func (s *serviceLatency) Start(measurementWg *sync.WaitGroup) error {
	var err error
	defer measurementWg.Done()

	s.LatencyQuantiles, s.NormLatencies = nil, nil
	if err := deployPodInNamespace(
		s.ClientSet,
		types.SvcLatencyNs,
		types.SvcLatencyCheckerName,
		"quay.io/kube-burner/fedora-nc:latest",
		[]string{"sleep", "inf"},
	); err != nil {
		return err
	}
	if s.svcLatencyChecker, err = mutil.NewSvcLatencyChecker(s.ClientSet, *s.RestConfig); err != nil {
		return fmt.Errorf("error creating SvcLatencyChecker: %v", err)
	}
	sgvr, err := kutil.ResourceToGVR(s.RestConfig, "Service", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "Service", err)
	}

	// Create shared informer factory for typed clients
	clientset := kubernetes.NewForConfigOrDie(s.RestConfig)
	factory := informers.NewSharedInformerFactory(clientset, 0)
	// EndpointSlice/Service lister & informer from typed client
	s.epsLister = factory.Discovery().V1().EndpointSlices().Lister()
	s.svcLister = factory.Core().V1().Services().Lister()
	epsInformer := factory.Discovery().V1().EndpointSlices().Informer()
	svcInformer := factory.Core().V1().Services().Informer()

	// Set transforms to reduce memory footprint
	if err := epsInformer.SetTransform(endpointSliceTransformFunc()); err != nil {
		log.Warnf("failed to set EndpointSlice transform: %v", err)
	}
	if err := svcInformer.SetTransform(serviceTypedTransformFunc()); err != nil {
		log.Warnf("failed to set Service transform: %v", err)
	}

	// Start the informer
	s.stopInformerCh = make(chan struct{})
	factory.Start(s.stopInformerCh)

	// Wait for the endpoint informer cache to sync
	if !cache.WaitForCacheSync(s.stopInformerCh, epsInformer.HasSynced) {
		return fmt.Errorf("failed to sync endpoint informer cache")
	}
	if !cache.WaitForCacheSync(s.stopInformerCh, svcInformer.HasSynced) {
		return fmt.Errorf("failed to sync service informer cache")
	}
	s.startMeasurement([]MeasurementWatcher{
		{
			dynamicClient: dynamic.NewForConfigOrDie(s.RestConfig),
			name:          "svcWatcher",
			resource:      sgvr,
			handlers: &cache.ResourceEventHandlerFuncs{
				AddFunc: s.handleCreateSvc,
			},
			transform: serviceTransformFunc(),
		},
	})
	return nil
}

func (s *serviceLatency) Stop() error {
	// 5 minutes should be more than enough to cleanup this namespace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer func() {
		cancel()
		s.stopWatchers()
		close(s.stopInformerCh)
	}()
	kutil.CleanupNamespacesByLabel(ctx, s.ClientSet, fmt.Sprintf("kubernetes.io/metadata.name=%s", types.SvcLatencyNs))
	s.normalizeMetrics()
	for _, q := range s.LatencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		// Divide nanoseconds by 1e6 to get milliseconds
		log.Infof("serviceLatency: %s: %s 99th: %d ms max: %d ms avg: %d ms", s.JobConfig.Name, pq.QuantileName, pq.P99/1e6, pq.Max/1e6, pq.Avg/1e6)
	}
	return nil
}

func (s *serviceLatency) normalizeMetrics() {
	var latencies []float64
	var ipAssignedLatencies []float64
	sLen := 0
	s.Metrics.Range(func(key, value any) bool {
		sLen++
		metric := value.(svcMetric)
		latencies = append(latencies, float64(metric.ReadyLatency))
		s.NormLatencies = append(s.NormLatencies, metric)
		if metric.IPAssignedLatency != 0 {
			ipAssignedLatencies = append(ipAssignedLatencies, float64(metric.IPAssignedLatency))
		}
		return true
	})
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = s.Uuid
		latencySummary.Timestamp = time.Now().UTC()
		latencySummary.Metadata = s.Metadata
		latencySummary.MetricName = svcLatencyQuantilesMeasurement
		latencySummary.JobName = s.JobConfig.Name
		return latencySummary
	}
	if sLen > 0 {
		s.LatencyQuantiles = append(s.LatencyQuantiles, calcSummary("Ready", latencies))
	}
	if len(ipAssignedLatencies) > 0 {
		s.LatencyQuantiles = append(s.LatencyQuantiles, calcSummary("IPAssigned", ipAssignedLatencies))
	}
}

func (s *serviceLatency) waitForEndpointSlices(svc *corev1.Service) error {
	err := wait.PollUntilContextCancel(context.TODO(), 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		if s.epsLister == nil {
			return true, nil
		}
		endpointSlices, err := s.epsLister.EndpointSlices(svc.Namespace).List(labels.SelectorFromSet(map[string]string{"kubernetes.io/service-name": svc.Name}))
		if err != nil {
			return false, nil
		}
		for _, endpointSlice := range endpointSlices {
			for _, endpoint := range endpointSlice.Endpoints {
				if len(endpoint.Addresses) > 0 {
					return true, nil
				}
			}
		}
		return false, nil
	})
	return err
}

func (s *serviceLatency) waitForIngress(svc *corev1.Service) error {
	err := wait.PollUntilContextCancel(context.TODO(), 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		svc, err := s.svcLister.Services(svc.Namespace).Get(svc.Name)
		if err != nil {
			return false, nil
		}
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			return true, nil
		}
		return false, nil
	})
	return err
}

func (s *serviceLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

func (s *serviceLatency) IsCompatible() bool {
	_, exists := supportedServiceLatencyJobTypes[s.JobConfig.JobType]
	return exists
}

// serviceTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels, annotations (config.KubeBurnerLabelServiceLatency only)
// - spec: type, clusterIPs, ports
// - status: loadBalancer.ingress
func serviceTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := createMinimalUnstructured(u, defaultMetadataTransformOpts())

		if annotations := u.GetAnnotations(); annotations != nil {
			if val, exists := annotations[config.KubeBurnerLabelServiceLatency]; exists {
				_ = unstructured.SetNestedField(minimal.Object, map[string]interface{}{
					config.KubeBurnerLabelServiceLatency: val,
				}, "metadata", "annotations")
			}
		}

		if svcType, found, _ := unstructured.NestedString(u.Object, "spec", "type"); found {
			_ = unstructured.SetNestedField(minimal.Object, svcType, "spec", "type")
		}
		if clusterIPs, found, _ := unstructured.NestedStringSlice(u.Object, "spec", "clusterIPs"); found {
			ips := make([]interface{}, len(clusterIPs))
			for i, ip := range clusterIPs {
				ips[i] = ip
			}
			_ = unstructured.SetNestedSlice(minimal.Object, ips, "spec", "clusterIPs")
		}
		if ports, found, _ := unstructured.NestedSlice(u.Object, "spec", "ports"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, ports, "spec", "ports")
		}
		if ingress, found, _ := unstructured.NestedSlice(u.Object, "status", "loadBalancer", "ingress"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, ingress, "status", "loadBalancer", "ingress")
		}

		return minimal, nil
	}
}

// serviceTypedTransformFunc preserves the following fields for typed Service informer:
// - metadata: name, namespace, uid
// - status: loadBalancer (needed for waitForIngress)
func serviceTypedTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return obj, nil
		}
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svc.Name,
				Namespace: svc.Namespace,
				UID:       svc.UID,
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: svc.Status.LoadBalancer,
			},
		}, nil
	}
}

// endpointSliceTransformFunc preserves the following fields for typed EndpointSlice informer:
// - metadata: name, namespace, labels (needed for label selector lookup)
// - endpoints (needed to check addresses in waitForEndpointSlices)
func endpointSliceTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		eps, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			return obj, nil
		}
		return &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      eps.Name,
				Namespace: eps.Namespace,
				Labels:    eps.Labels,
			},
			Endpoints: eps.Endpoints,
		}, nil
	}
}
