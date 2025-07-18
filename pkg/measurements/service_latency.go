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

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/measurements/util"
	kutil "github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	lcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	svcLatencyMeasurement          = "svcLatencyMeasurement"
	svcLatencyQuantilesMeasurement = "svcLatencyQuantilesMeasurement"
)

type serviceLatency struct {
	BaseMeasurement

	epLister  lcorev1.EndpointsLister
	svcLister lcorev1.ServiceLister
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

func newServiceLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	if measurement.ServiceTimeout == 0 {
		return nil, fmt.Errorf("svcTimeout cannot be 0")
	}

	return serviceLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (slmf serviceLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &serviceLatency{
		BaseMeasurement: slmf.NewBaseLatency(jobConfig, clientSet, restConfig, svcLatencyMeasurement, svcLatencyQuantilesMeasurement, embedCfg),
	}
}

func (s *serviceLatency) handleCreateSvc(obj any) {
	// TODO Magic annotation to skip service
	svc := obj.(*corev1.Service)
	if annotation, ok := svc.Annotations["kube-burner.io/service-latency"]; ok {
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
		if err := s.waitForEndpoints(svc); err != nil {
			log.Fatal(err)
		}
		endpointsReadyTs := time.Now().UTC()
		log.Debugf("Endpoints %v/%v ready", svc.Namespace, svc.Name)
		svcLatencyChecker, err := util.NewSvcLatencyChecker(s.ClientSet, *s.RestConfig)
		if err != nil {
			log.Error(err)
		}
		for _, specPort := range svc.Spec.Ports {
			if specPort.Protocol == corev1.ProtocolTCP { // Support TCP protocol
				switch svc.Spec.Type {
				case corev1.ServiceTypeClusterIP:
					ips = svc.Spec.ClusterIPs
					port = specPort.Port
				case corev1.ServiceTypeNodePort:
					ips = []string{svcLatencyChecker.Pod.Status.HostIP}
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
					err = svcLatencyChecker.Ping(ip, port, s.Config.ServiceTimeout)
					if err != nil {
						log.Error(err)
						return
					}
				}
			}
		}
		svcLatency := time.Since(endpointsReadyTs)
		log.Debugf("Service %v/%v latency was: %vms", svc.Namespace, svc.Name, svcLatency.Milliseconds())
		s.metrics.Store(string(svc.UID), svcMetric{
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
	// Reset latency slices, required in multi-job benchmarks
	s.latencyQuantiles, s.normLatencies = nil, nil
	defer measurementWg.Done()
	err := deployPodInNamespace(s.ClientSet, types.SvcLatencyNs, types.SvcLatencyCheckerName, "quay.io/cloud-bulldozer/fedora-nc:latest", []string{"sleep", "inf"})
	if err != nil {
		return err
	}
	s.startMeasurement(
		[]MeasurementWatcher{
			{
				restClient:    s.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
				name:          "svcWatcher",
				resource:      "services",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", s.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: s.handleCreateSvc,
				},
			},
			{
				restClient:    s.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
				name:          "epWatcher",
				resource:      "endpoints",
				labelSelector: "",
				handlers:      nil,
			},
		},
	)
	s.svcLister = lcorev1.NewServiceLister(s.watchers[0].Informer.GetIndexer())
	s.epLister = lcorev1.NewEndpointsLister(s.watchers[1].Informer.GetIndexer())
	return nil
}

func (s *serviceLatency) Stop() error {
	// 5 minutes should be more than enough to cleanup this namespace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer func() {
		cancel()
		s.stopWatchers()
	}()
	kutil.CleanupNamespaces(ctx, s.ClientSet, fmt.Sprintf("kubernetes.io/metadata.name=%s", types.SvcLatencyNs))
	s.normalizeMetrics()
	for _, q := range s.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		// Divide nanoseconds by 1e6 to get milliseconds
		log.Infof("%s: %s 99th: %dms max: %dms avg: %dms", s.JobConfig.Name, pq.QuantileName, pq.P99/1e6, pq.Max/1e6, pq.Avg/1e6)
	}
	return nil
}

func (s *serviceLatency) normalizeMetrics() {
	var latencies []float64
	var ipAssignedLatencies []float64
	sLen := 0
	s.metrics.Range(func(key, value any) bool {
		sLen++
		metric := value.(svcMetric)
		latencies = append(latencies, float64(metric.ReadyLatency))
		s.normLatencies = append(s.normLatencies, metric)
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
		s.latencyQuantiles = append(s.latencyQuantiles, calcSummary("Ready", latencies))
	}
	if len(ipAssignedLatencies) > 0 {
		s.latencyQuantiles = append(s.latencyQuantiles, calcSummary("IPAssigned", ipAssignedLatencies))
	}
}

func (s *serviceLatency) waitForEndpoints(svc *corev1.Service) error {
	err := wait.PollUntilContextCancel(context.TODO(), 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		endpoints, err := s.epLister.Endpoints(svc.Namespace).Get(svc.Name)
		if err != nil {
			return false, nil
		}
		for _, subset := range endpoints.Subsets {
			if len(subset.Addresses) > 0 {
				return true, nil
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
