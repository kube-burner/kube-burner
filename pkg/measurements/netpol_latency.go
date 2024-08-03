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
	"strings"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/measurements/util"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	lcorev1 "k8s.io/client-go/listers/core/v1"
	lnetv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	netpolLatencyMeasurement          = "netpolLatencyMeasurement"
	netpolLatencyQuantilesMeasurement = "netpolLatencyQuantilesMeasurement"
)

type netpolLatency struct {
	config           types.Measurement
	netpolWatcher       *metrics.Watcher
	podWatcher        *metrics.Watcher
	podLister         lcorev1.PodLister
	netpolLister        lnetv1.NetworkPolicyLister
	metrics          map[string]netpolMetric
	latencyQuantiles []interface{}
	normLatencies    []interface{}
	metricLock       sync.RWMutex
}

type netpolMetric struct {
	Timestamp         time.Time          `json:"timestamp"`
	ReadyLatency      time.Duration      `json:"ready"`
	MetricName        string             `json:"metricName"`
	UUID              string             `json:"uuid"`
	Namespace         string             `json:"namespace"`
	Name              string             `json:"netpol"`
	Metadata          interface{}        `json:"metadata,omitempty"`
}

func init() {
	measurementMap["netpolLatency"] = &netpolLatency{
		metrics: map[string]netpolMetric{},
	}
}

func (n *netpolLatency) getPodList(ns string, ps *metav1.LabelSelector) *corev1.PodList {
	listOptions := metav1.ListOptions{}
	if ps != nil {
		selector, err := metav1.LabelSelectorAsSelector(ps)
		if err != nil {
			log.Fatal(err)
		}
		if n.config.SkipPodWait == false {
			if err := n.waitForPods(ns, selector); err != nil {
				log.Fatal(err)
			}
		}
		listOptions.LabelSelector = selector.String()

	}

	pods, err := factory.clientSet.CoreV1().Pods(ns).List(context.TODO(), listOptions)
	if err != nil {
		log.Fatal(err)
	}
	return pods
}

func (n *netpolLatency) handleCreateNetpol(obj interface{}) {
	// TODO Magic annotation to skip netpol
	netpol := obj.(*networkingv1.NetworkPolicy)
	if annotation, ok := netpol.Annotations["kube-burner.io/netpol-latency"]; ok {
		if annotation == "false" {
			log.Debugf("Annotation found, discarding netpol %v/%v", netpol.Namespace, netpol.Name)
		}
	}
	log.Debugf("Handling netpol: %v/%v", netpol.Namespace, netpol.Name)
	go func(netpol *networkingv1.NetworkPolicy) {
		var localIps []string
		var ingressPort int32 = 8080

		// single string with all local IPs
		var formattedIps []string
		// Retrieve local pods. For ingress policy, peer pods send request to local pods
		localPods := n.getPodList(netpol.Namespace, &netpol.Spec.PodSelector)
		for _, localPod := range localPods.Items {
			if localPod.Status.Phase == corev1.PodRunning {
				localIps = append(localIps, localPod.Status.PodIP)
				formattedIps = append(formattedIps, fmt.Sprintf("%s", localPod.Status.PodIP))

			}
		}
		allIpAddress := strings.Join(formattedIps, " ")

		netpolReadyTs := time.Now().UTC()
		log.Debugf("Network Policy Object %v/%v is ready ", netpol.Namespace, netpol.Name)
		for _, ingress := range netpol.Spec.Ingress {
			// Let's target only first TCP port i.e peer pod send request to local pod's first port.
			for _, port := range ingress.Ports {
				if *port.Protocol == corev1.ProtocolTCP {
					ingressPort = port.Port.IntVal
					break
				}
			}
			for _, from := range ingress.From {
				// Read all peer namespaces listed in the netpol spec.
				// Use local namespace as peer if from.NamespaceSelector is empty and from.PodSelector defines peer pods on local namespace
				var peerNsList []string
				if from.NamespaceSelector != nil || from.PodSelector != nil {
					if from.NamespaceSelector != nil {
						if from.NamespaceSelector.MatchLabels != nil {
							if peerNs, exists := from.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]; exists {
								peerNsList = append(peerNsList, peerNs)
							}
						} else {
							for _, expr := range from.NamespaceSelector.MatchExpressions {
								// Let's restrict testing to IN operator only for now
								if expr.Key == "kubernetes.io/metadata.name" {
									if expr.Operator == metav1.LabelSelectorOpIn {
										peerNsList = expr.Values
									} else {
										log.Errorf("Netpol workload supports only IN operator for NamespaceSelector.MatchExpressions")
									}
								}
							}
						}
					} else {
						peerNsList = append(peerNsList, netpol.Namespace)
					}
					// Iterate through peer pods on peer namespaces and issue request to local pods for ingress netpol latency testing
					for _, peerNs := range peerNsList {
						peerPods := n.getPodList(peerNs, from.PodSelector)
						for _, peerPod := range peerPods.Items {
							if peerPod.Status.Phase == corev1.PodRunning {
								retryIpAddresses := allIpAddress
								maxAttempts := 3
								var err error = nil
								for attempt := 1; attempt <= maxAttempts; attempt++ {
									retryIpAddresses, err = util.Ping(&peerPod, factory.clientSet, *factory.restConfig, retryIpAddresses, ingressPort, n.config.NetpolTimeout)
									if err != nil {
										log.Error(err)
										return
									}
									if retryIpAddresses == "" {
										break
									}
								}
								if retryIpAddresses != "" {
									log.Debugf("Connecting to local %s failed after 3 attempts", retryIpAddresses)
								}
							}
						}
					}
				}
			}
		}
		// ignore measuring metric for deny all network policy
		if len(netpol.Spec.Ingress) == 0 && len(netpol.Spec.Egress) == 0 {
			return
		}

		npolLatency := time.Since(netpolReadyTs)
		log.Debugf("Network Policy %v/%v latency was: %vms", netpol.Namespace, netpol.Name, npolLatency.Milliseconds())
		n.metricLock.Lock()
		n.metrics[string(netpol.UID)] = netpolMetric{
			Name:              netpol.Name,
			Namespace:         netpol.Namespace,
			Timestamp:         netpol.CreationTimestamp.Time.UTC(),
			MetricName:        netpolLatencyMeasurement,
			ReadyLatency:      npolLatency,
			UUID:              globalCfg.UUID,
			Metadata:          factory.metadata,
		}
		n.metricLock.Unlock()
	}(netpol)
}

func (n *netpolLatency) setConfig(cfg types.Measurement) {
	n.config = cfg
}
func (n *netpolLatency) validateConfig() error {
	if n.config.NetpolTimeout == 0 {
		return fmt.Errorf("NetpolTimeout cannot be 0")
	}
	return nil
}

// start netpol latency measurement
func (n *netpolLatency) start(measurementWg *sync.WaitGroup) error {
	// Reset latency slices, required in multi-job benchmarks
	n.latencyQuantiles, n.normLatencies = nil, nil
	defer measurementWg.Done()
	log.Infof("Creating netpol latency watcher for %s", factory.jobConfig.Name)
	n.netpolWatcher = metrics.NewNetworkWatcher(
		factory.clientSet,
		"netpolWatcher",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-uuid=%v", globalCfg.UUID)
		},
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	n.netpolWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: n.handleCreateNetpol,
	})
	n.podWatcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"podWatcher",
		"pods",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", globalCfg.RUNID)
		},
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	// Use an endpoints lister to reduce and optimize API interactions in waitForEndpoints
	n.netpolLister = lnetv1.NewNetworkPolicyLister(n.netpolWatcher.Informer.GetIndexer())
	n.podLister = lcorev1.NewPodLister(n.podWatcher.Informer.GetIndexer())
	if err := n.netpolWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("Network Policy Latency measurement error: %s", err)
	}
	if err := n.podWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("Network Policy (podWatcher) Latency measurement error: %s", err)
	}
	return nil
}

func (n *netpolLatency) stop() error {
	_, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer func() {
		cancel()
		n.netpolWatcher.StopWatcher()
		n.podWatcher.StopWatcher()
	}()
	n.normalizeMetrics()
	for _, q := range n.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		// Divide nanoseconds by 1e6 to get milliseconds
		log.Infof("%s: %s 50th: %dms 99th: %dms max: %dms avg: %dms", factory.jobConfig.Name, pq.QuantileName, pq.P50/1e6, pq.P99/1e6, pq.Max/1e6, pq.Avg/1e6)
	}
	return nil
}

func (n *netpolLatency) normalizeMetrics() {
	var latencies []float64
	var ipAssignedLatencies []float64
	for _, metric := range n.metrics {
		latencies = append(latencies, float64(metric.ReadyLatency))
		n.normLatencies = append(n.normLatencies, metric)
	}
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = globalCfg.UUID
		latencySummary.Timestamp = time.Now().UTC()
		latencySummary.Metadata = factory.metadata
		latencySummary.MetricName = netpolLatencyQuantilesMeasurement
		return latencySummary
	}
	if len(n.metrics) > 0 {
		n.latencyQuantiles = append(n.latencyQuantiles, calcSummary("Ready", latencies))
	}
	if len(ipAssignedLatencies) > 0 {
		n.latencyQuantiles = append(n.latencyQuantiles, calcSummary("IPAssigned", ipAssignedLatencies))
	}
}

func (n *netpolLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]interface{}{
		netpolLatencyMeasurement:          n.normLatencies,
		netpolLatencyQuantilesMeasurement: n.latencyQuantiles,
	}
	indexLatencyMeasurement(n.config, jobName, metricMap, indexerList)
}

func (n *netpolLatency) waitForPods(ns string, s labels.Selector) error {
	err := wait.PollUntilContextCancel(context.TODO(), 1000*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		pods, err := n.podLister.Pods(ns).List(s)
		if err != nil {
			return false, nil
		}
		if len(pods) > 0 {
			for _, pod := range pods {
				if pod.Status.Phase != corev1.PodRunning {
					return false, nil
				}
			}
			return true, nil
		}
		return false, nil
	})
	return err
}

func (n *netpolLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}
