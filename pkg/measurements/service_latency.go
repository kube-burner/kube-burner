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

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/measurements/util"
	kutil "github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	lcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	svcLatencyMetric               = "svcLatencyMeasurement"
	svcLatencyQuantilesMeasurement = "svcLatencyQuantilesMeasurement"
)

type serviceLatency struct {
	config           types.Measurement
	svcWatcher       *metrics.Watcher
	epWatcher        *metrics.Watcher
	epLister         lcorev1.EndpointsLister
	svcLister        lcorev1.ServiceLister
	metrics          map[string]svcMetric
	latencyQuantiles []interface{}
	normLatencies    []interface{}
	metricLock       sync.RWMutex
}

type svcMetric struct {
	Timestamp         time.Time          `json:"timestamp"`
	IPAssignedLatency time.Duration      `json:"ipAssigned,omitempty"`
	ReadyLatency      time.Duration      `json:"ready"`
	MetricName        string             `json:"metricName"`
	JobConfig         config.Job         `json:"jobConfig"`
	UUID              string             `json:"uuid"`
	Namespace         string             `json:"namespace"`
	Name              string             `json:"service"`
	Metadata          interface{}        `json:"metadata,omitempty"`
	ServiceType       corev1.ServiceType `json:"type"`
}

func init() {
	measurementMap["serviceLatency"] = &serviceLatency{
		metrics: map[string]svcMetric{},
	}
}

func deployAssets() error {
	var err error
	if err = kutil.CreateNamespace(factory.clientSet, types.SvcLatencyNs, nil, nil); err != nil {
		return err
	}
	if _, err = factory.clientSet.CoreV1().Pods(types.SvcLatencyNs).Create(context.TODO(), types.SvcLatencyCheckerPod, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Warn(err)
		} else {
			return err
		}
	}
	err = wait.PollUntilContextCancel(context.TODO(), 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		pod, err := factory.clientSet.CoreV1().Pods(types.SvcLatencyNs).Get(context.TODO(), types.SvcLatencyCheckerPod.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}
		return true, nil
	})
	return err
}

func (s *serviceLatency) handleCreateSvc(obj interface{}) {
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
		svcLatencyChecker, err := util.NewSvcLatencyChecker(factory.clientSet, *factory.restConfig)
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
					err = svcLatencyChecker.Ping(ip, port, s.config.ServiceTimeout)
					if err != nil {
						log.Error(err)
						return
					}
				}
			}
		}
		svcLatency := time.Since(endpointsReadyTs)
		log.Debugf("Service %v/%v latency was: %vms", svc.Namespace, svc.Name, svcLatency.Milliseconds())
		s.metricLock.Lock()
		s.metrics[string(svc.UID)] = svcMetric{
			Name:              svc.Name,
			Namespace:         svc.Namespace,
			Timestamp:         svc.CreationTimestamp.Time.UTC(),
			MetricName:        svcLatencyMetric,
			ServiceType:       svc.Spec.Type,
			ReadyLatency:      svcLatency,
			JobConfig:         *factory.jobConfig,
			UUID:              globalCfg.UUID,
			Metadata:          factory.metadata,
			IPAssignedLatency: ipAssignedLatency,
		}
		s.metricLock.Unlock()
	}(svc)
}

func (s *serviceLatency) setConfig(cfg types.Measurement) {
	s.config = cfg
}
func (s *serviceLatency) validateConfig() error {
	if s.config.ServiceTimeout == 0 {
		return fmt.Errorf("svcTimeout cannot be 0")
	}
	return nil
}

// start service latency measurement
func (s *serviceLatency) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Reset latency slices, required in multi-job benchmarks
	s.latencyQuantiles, s.normLatencies = nil, nil
	if err := deployAssets(); err != nil {
		log.Fatal(err)
		return err
	}
	log.Infof("Creating service latency watcher for %s", factory.jobConfig.Name)
	s.svcWatcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"svcWatcher",
		"services",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", globalCfg.RUNID)
		},
		cache.Indexers{},
	)
	s.svcWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: s.handleCreateSvc,
	})
	s.epWatcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"epWatcher",
		"endpoints",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {},
		cache.Indexers{},
	)
	// Use an endpoints lister to reduce and optimize API interactions in waitForEndpoints
	s.svcLister = lcorev1.NewServiceLister(s.svcWatcher.Informer.GetIndexer())
	s.epLister = lcorev1.NewEndpointsLister(s.epWatcher.Informer.GetIndexer())
	if err := s.svcWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("Service Latency measurement error: %s", err)
	}
	if err := s.epWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("Service Latency measurement error: %s", err)
	}
	return nil
}

func (s *serviceLatency) stop() error {
	s.svcWatcher.StopWatcher()
	s.epWatcher.StopWatcher()
	// 5 minutes should be more than enough to cleanup this namespace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	kutil.CleanupNamespaces(ctx, factory.clientSet, fmt.Sprintf("kubernetes.io/metadata.name=%s", types.SvcLatencyNs))
	s.normalizeMetrics()
	for _, q := range s.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		// Divide nanoseconds by 1e6 to get milliseconds
		log.Infof("%s: %s 50th: %dms 99th: %dms max: %dms avg: %dms", factory.jobConfig.Name, pq.QuantileName, pq.P50/1e6, pq.P99/1e6, pq.Max/1e6, pq.Avg/1e6)
	}
	return nil
}

func (s *serviceLatency) normalizeMetrics() {
	var latencies []float64
	var ipAssignedLatencies []float64
	for _, metric := range s.metrics {
		latencies = append(latencies, float64(metric.ReadyLatency))
		s.normLatencies = append(s.normLatencies, metric)
		if metric.IPAssignedLatency != 0 {
			ipAssignedLatencies = append(ipAssignedLatencies, float64(metric.IPAssignedLatency))
		}
	}
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = globalCfg.UUID
		latencySummary.JobConfig = *factory.jobConfig
		latencySummary.Timestamp = time.Now().UTC()
		latencySummary.Metadata = factory.metadata
		latencySummary.MetricName = svcLatencyQuantilesMeasurement
		return latencySummary
	}
	if len(s.metrics) > 0 {
		s.latencyQuantiles = append(s.latencyQuantiles, calcSummary("Ready", latencies))
	}
	if len(ipAssignedLatencies) > 0 {
		s.latencyQuantiles = append(s.latencyQuantiles, calcSummary("IPAssigned", ipAssignedLatencies))
	}
}

func (s *serviceLatency) index(indexer indexers.Indexer, jobName string) {
	metricMap := map[string][]interface{}{
		svcLatencyMetric:               s.normLatencies,
		svcLatencyQuantilesMeasurement: s.latencyQuantiles,
	}
	for metricName, documents := range metricMap {
		indexingOpts := indexers.IndexingOpts{
			MetricName: fmt.Sprintf("%s-%s", metricName, jobName),
		}
		log.Debugf("Indexing [%d] documents: %s", len(documents), metricName)
		resp, err := indexer.Index(documents, indexingOpts)
		if err != nil {
			log.Error(err.Error())
		} else {
			log.Info(resp)
		}
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

func (s *serviceLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}
