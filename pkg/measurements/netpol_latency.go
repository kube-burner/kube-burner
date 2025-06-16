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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"maps"
	"slices"

	kconfig "github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/measurements/util"
	kutil "github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubectl/pkg/scheme"
)

const (
	networkPolicyProxyPort            = "9002"
	networkPolicyProxy                = "network-policy-proxy"
	serverPingTestPort                = 8080
	netpolLatencyMeasurement          = "netpolLatencyMeasurement"
	netpolLatencyQuantilesMeasurement = "netpolLatencyQuantilesMeasurement"
)

var proxyPortForwarder *util.PodPortForwarder
var nsPodAddresses = make(map[string]map[string][]string)
var proxyEndpoint string

type connection struct {
	Addresses []string `json:"addresses"`
	Ports     []int32  `json:"ports"`
	Netpol    string   `json:"netpol"`
}

var connections = make(map[string][]connection)

type connTest struct {
	Address    string    `json:"address"`
	Port       int32     `json:"port"`
	IngressIdx int       `json:"connectionidx"`
	NpName     string    `json:"npname"`
	Timestamp  time.Time `json:"timestamp"`
}

// Network policies in the same namespace might be defining same peer namespace pods in their spec, "addrReuse" is used to handle this
var addrReuse = make(map[string][]string)
var clusterResults = make(map[string][]connTest)
var npResults = make(map[string][]float64)
var npCreationTime = make(map[string]time.Time)

type ProxyResponse struct {
	Result bool `json:"result"`
}

type netpolLatency struct {
	BaseMeasurement
}

type netpolMetric struct {
	Timestamp       time.Time     `json:"timestamp"`
	MinReadyLatency time.Duration `json:"minReady"`
	ReadyLatency    time.Duration `json:"ready"`
	MetricName      string        `json:"metricName"`
	UUID            string        `json:"uuid"`
	Namespace       string        `json:"namespace"`
	Name            string        `json:"netpol"`
	Metadata        any           `json:"metadata,omitempty"`
	JobName         string        `json:"jobName,omitempty"`
}

type netpolLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newNetpolLatencyMeasurementFactory(configSpec kconfig.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	return netpolLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (nplmf netpolLatencyMeasurementFactory) NewMeasurement(jobConfig *kconfig.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &netpolLatency{
		BaseMeasurement: nplmf.NewBaseLatency(jobConfig, clientSet, restConfig, netpolLatencyMeasurement, netpolLatencyQuantilesMeasurement, embedCfg),
	}
}

func yamlToUnstructured(fileName string, y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvk, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatalf("Error decoding YAML (%s): %s", fileName, err)
	}
	return o, gvk
}

func getNamespacesByLabel(s *metav1.LabelSelector) []string {
	var namespaces []string
	if s.MatchLabels != nil {
		if ns, exists := s.MatchLabels["kubernetes.io/metadata.name"]; exists {
			namespaces = append(namespaces, ns)
		}
	} else {
		for _, expr := range s.MatchExpressions {
			// Let's restrict testing to IN operator only for now
			if expr.Key == "kubernetes.io/metadata.name" {
				if expr.Operator == metav1.LabelSelectorOpIn {
					namespaces = expr.Values
				} else {
					log.Errorf("Netpol workload supports only IN operator for NamespaceSelector.MatchExpressions")
				}
			}
		}
	}
	return namespaces
}

// For the give namespace and pod selector, retrieve the pods and their addresses,
// and then store them in nsPodAddresses. Network policies in different namespaces
// might be using the same remote namespace pod selector. As the pod addresses for
// the pod selector already stored in nsPodAddresses, fetches by the later
// namespaces can use from nsPodAddresses instead of querying kube-apiserver
func addPodsByLabel(clientSet kubernetes.Interface, ns string, ps *metav1.LabelSelector) []string {
	var addresses []string
	selectorString := ns
	listOptions := metav1.ListOptions{}
	if ns == "" {
		return nil
	}
	if ps != nil {
		selector, err := metav1.LabelSelectorAsSelector(ps)
		if err != nil {
			log.Fatal(err)
		}
		listOptions.LabelSelector = selector.String()
		selectorString = selector.String()
	}
	if podsMap, ok := nsPodAddresses[ns]; !ok || podsMap[selectorString] == nil {
		pods, err := clientSet.CoreV1().Pods(ns).List(context.TODO(), listOptions)
		if err != nil {
			log.Fatal(err)
		}
		for _, pod := range pods.Items {
			addresses = append(addresses, pod.Status.PodIP)
		}
		if !ok {
			nsPodAddresses[ns] = map[string][]string{selectorString: addresses}
		} else {
			nsPodAddresses[ns][selectorString] = addresses
		}
	} else {
		addresses = nsPodAddresses[ns][selectorString]
	}
	return addresses
}

// Record the network policy creation timestamp when it is created.
// We later do a diff with successful connection timestamp and define that as a network policy programming latency.
func (n *netpolLatency) handleCreateNetpol(obj any) {
	netpol := obj.(*networkingv1.NetworkPolicy)
	npCreationTime[netpol.Name] = netpol.CreationTimestamp.UTC()
}

// Render the network policy from the object template using iteration details as input
func (n *netpolLatency) getNetworkPolicy(iteration int, replica int, obj kconfig.Object, objectSpec []byte) *networkingv1.NetworkPolicy {

	templateData := map[string]any{
		"JobName":   n.JobConfig.Name,
		"Iteration": strconv.Itoa(iteration),
		"UUID":      n.Uuid,
		"Replica":   strconv.Itoa(replica),
	}
	maps.Copy(templateData, obj.InputVars)
	renderedObj, err := kutil.RenderTemplate(objectSpec, templateData, kutil.MissingKeyError, []string{})
	if err != nil {
		log.Fatalf("Template error in %s: %s", obj.ObjectTemplate, err)
	}

	networkingv1.AddToScheme(scheme.Scheme)
	decode := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer().Decode
	netpolObj, _, err := decode(renderedObj, nil, nil)
	if err != nil {
		log.Fatalf("Failed to decode YAML file: %v", err)
	}

	networkPolicy, ok := netpolObj.(*networkingv1.NetworkPolicy)
	if !ok {
		log.Fatalf("YAML file is not a SecurityGroup")
	}
	return networkPolicy
}

// It parses the network policy object for each iteration and identify the remote pods from which the namespace has to allow ingress traffic.
// To test ingress, remote pods will be the client pods which send requests to current namespace local pods on which ingress policy is defined.
// For example, local pods in namespace-1 will be accepting http requests from remote namespace-2 pods when ingress-1 in namespace-1 specify namespace-2 as remote.
// connections is map of each client pod with list of addresses it has to send connection requests. Network policy name is also stored along with the addresses.
// Note: this measurement package only parses template to get IP addresses of pods but never creates network policies. kube-burner's "burner" package creates network policies
func (n *netpolLatency) prepareConnections() {
	// Reset latency slices, required in multi-job benchmarks
	for _, obj := range n.JobConfig.Objects {
		cleanTemplate, err := readTemplate(obj, n.EmbedCfg)
		if err != nil {
			log.Fatalf("Error in readTemplate %s: %s", obj.ObjectTemplate, err)
		}
		if getObjectType(obj, cleanTemplate) != "NetworkPolicy" {
			continue
		}
		for i := range n.JobConfig.JobIterations {
			for r := 1; r <= obj.Replicas; r++ {
				networkPolicy := n.getNetworkPolicy(i, r, obj, cleanTemplate)
				nsIndex := i / n.JobConfig.IterationsPerNamespace
				namespace := fmt.Sprintf("%s-%d", n.JobConfig.Namespace, nsIndex)
				localPods := addPodsByLabel(n.ClientSet, namespace, &networkPolicy.Spec.PodSelector)
				for _, ingress := range networkPolicy.Spec.Ingress {
					var ingressPorts []int32
					for _, from := range ingress.From {
						// support only selector based connection testing
						if from.NamespaceSelector == nil && from.PodSelector == nil {
							continue
						}
						for _, port := range ingress.Ports {
							if *port.Protocol != corev1.ProtocolTCP {
								log.Fatalf("Only TCP ports supported in testing")
							}
							// Server listens on only one port for ping test
							if port.Port.IntVal == serverPingTestPort {
								ingressPorts = append(ingressPorts, port.Port.IntVal)
								break
							}
						}
						namespaces := getNamespacesByLabel(from.NamespaceSelector)
						for _, namespace := range namespaces {
							remoteAddrs := addPodsByLabel(n.ClientSet, namespace, from.PodSelector)
							for _, ra := range remoteAddrs {
								// exclude sending connection request to same ip address
								otherIPs := []string{}
								for _, ip := range localPods {
									if ip != ra {
										// Network policies in the same namespace might use same remote address (i.e remote namespace pod)
										ar := fmt.Sprintf("%s:%s", ra, ip)
										if addrReuse[ar] == nil {
											addrReuse[ar] = []string{networkPolicy.Name}
											// Avoid a peer pod pinging multiple times to same local pod because of pod reuse by network policies
											otherIPs = append(otherIPs, ip)
										} else {
											// check if network policy doesn't exist in the addrReuse
											if !slices.Contains(addrReuse[ar], networkPolicy.Name) {
												addrReuse[ar] = append(addrReuse[ar], networkPolicy.Name)

											}
										}
									}
								}
								if len(otherIPs) == 0 {
									continue
								}
								conn := connection{
									Addresses: otherIPs,
									Ports:     ingressPorts,
									Netpol:    networkPolicy.Name,
								}
								if connections[ra] == nil {
									connections[ra] = []connection{conn}
								} else {
									connections[ra] = append(connections[ra], conn)
								}
							}
						}
					}
				}
			}
		}
	}
}

// Send connections information to proxy pod
func sendConnections() {
	data, err := json.Marshal(connections)
	if err != nil {
		log.Fatalf("Failed to marshal payload: %v", err)
	}

	url := fmt.Sprintf("http://%s/initiate", proxyEndpoint)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Debugf("Connection information sent successfully")
	}
	waitForCondition(fmt.Sprintf("http://%s/checkConnectionsStatus", proxyEndpoint))
}

// Periodically check if proxy pod finished communication with all client pods,
// during 1) sending connection information 2) retrieving results
func waitForCondition(url string) {
	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Fatalf("Timeout reached. Stopping further checks.")
			return
		case <-ticker.C:
			resp, err := http.Get(url)
			if err != nil {
				log.Fatalf("Failed to check status: %v", err)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Fatalf("Failed to read response body: %v", err)
				continue
			}
			var response ProxyResponse
			err = json.Unmarshal(body, &response)
			if err != nil {
				log.Fatalf("Error decoding response: %v", err)
				return
			}
			if response.Result {
				return
			}
		}
	}
}

// Get results from proxy pod and parse them.
func (n *netpolLatency) processResults() {
	serverURL := fmt.Sprintf("http://%s", proxyEndpoint)
	resp, err := http.Get(fmt.Sprintf("%s/stop", serverURL))
	if err != nil {
		log.Fatalf("Failed to retrieve results: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected status code: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Wait till proxy got results from all pods
	waitForCondition(fmt.Sprintf("%s/checkStopStatus", serverURL))

	// Retrieve the results
	resp, err = http.Get(fmt.Sprintf("%s/results", serverURL))
	if err != nil {
		log.Fatalf("Failed to retrieve results: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Unexpected status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	if err := json.Unmarshal(body, &clusterResults); err != nil {
		log.Fatalf("Failed to unmarshal results: %v", err)
	}
	log.Tracef("clusterResults %v", clusterResults)
	for pod, results := range clusterResults {
		for _, res := range results {
			ar := fmt.Sprintf("%s:%s", pod, res.Address)
			for _, netpolName := range addrReuse[ar] {
				connLatency := float64(res.Timestamp.Sub(npCreationTime[netpolName]).Milliseconds())
				if _, ok := npResults[netpolName]; !ok {
					npResults[netpolName] = []float64{}
				}
				if connLatency < 0 {
					log.Warnf("🚨 Network policy %s has negitive latency %v for connection test from %v to %v", netpolName, connLatency, pod, res.Address)
				}
				npResults[netpolName] = append(npResults[netpolName], connLatency)
			}
		}
	}
	resp.Body.Close()
	for name, npl := range npResults {
		latencySummary := metrics.NewLatencySummary(npl, name)
		log.Tracef("netpol %s latency slice %v\n", name, npl)
		log.Tracef("%s: 50th: %d 95th: %d 99th: %d min: %d max: %d avg: %d\n", name, latencySummary.P50, latencySummary.P95, latencySummary.P99, latencySummary.Min, latencySummary.Max, latencySummary.Avg)

		// Use minVal for reporting as ping test tool might have delayed initiating ping tests from some remote addresses.
		// This can happen when remote pod was busy pinging other pods before trying our network policy pod
		n.metrics.Store(name, netpolMetric{
			Name:            name,
			Timestamp:       npCreationTime[name],
			MetricName:      netpolLatencyMeasurement,
			MinReadyLatency: time.Duration(latencySummary.Min),
			ReadyLatency:    time.Duration(latencySummary.Max),
			UUID:            n.Uuid,
			Metadata:        n.Metadata,
			JobName:         n.JobConfig.Name,
		})
	}
}

// Read network policy object template
func readTemplate(o kconfig.Object, embedCfg *fileutils.EmbedConfiguration) ([]byte, error) {
	f, err := fileutils.GetWorkloadReader(o.ObjectTemplate, embedCfg)
	if err != nil {
		log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
	}
	t, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
	}
	return t, err
}

func getObjectType(o kconfig.Object, t []byte) string {
	// Deserialize YAML
	cleanTemplate, err := kutil.CleanupTemplate(t)
	if err != nil {
		log.Fatalf("Error preparing template %s: %s", o.ObjectTemplate, err)
	}
	uns := &unstructured.Unstructured{}
	yamlToUnstructured(o.ObjectTemplate, cleanTemplate, uns)
	return uns.GetKind()
}

// Start network policy measurement
func (n *netpolLatency) Start(measurementWg *sync.WaitGroup) error {
	var err error
	defer measurementWg.Done()
	// Skip latency measurement for 1st job which creates only pods
	if value, ok := n.JobConfig.NamespaceLabels["kube-burner.io/skip-networkpolicy-latency"]; ok {
		if value == "true" {
			log.Debugf("Discarding network policy latency measurement for the job %v", n.JobConfig.Name)
			return nil
		}
	}
	_, err = n.ClientSet.CoreV1().Pods(networkPolicyProxy).Get(context.TODO(), networkPolicyProxy, metav1.GetOptions{})
	if err != nil {
		err = deployPodInNamespace(n.ClientSet, networkPolicyProxy, networkPolicyProxy, "quay.io/cloud-bulldozer/netpolproxy:latest", nil)
		if err != nil {
			return err
		}
	}
	if proxyPortForwarder == nil {
		proxyPortForwarder, err = util.NewPodPortForwarder(n.ClientSet, *n.RestConfig, networkPolicyProxyPort, networkPolicyProxy, networkPolicyProxy)
		if err != nil {
			return err
		}
		// Use proxyEndpoint to communicate with proxy pod
		proxyEndpoint = fmt.Sprintf("127.0.0.1:%s", networkPolicyProxyPort)
	}
	// Parse network policy template for each iteration and prepare connections for client pods
	n.prepareConnections()
	// send connection information to proxy to deliver to client pods
	if len(connections) > 0 {
		sendConnections()
	}

	n.startMeasurement(
		[]MeasurementWatcher{
			{
				restClient:    n.ClientSet.NetworkingV1().RESTClient().(*rest.RESTClient),
				name:          "netpolWatcher",
				resource:      "networkpolicies",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", n.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: n.handleCreateNetpol,
				},
			},
		},
	)

	return nil
}

func (n *netpolLatency) Stop() error {
	// Skip latency measurement for 1st job which creates only pods
	if value, ok := n.JobConfig.NamespaceLabels["kube-burner.io/skip-networkpolicy-latency"]; ok {
		if value == "true" {
			return nil
		}
	}
	if len(connections) > 0 {
		n.processResults()
	}
	proxyPortForwarder.CancelPodPortForwarder()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer func() {
		cancel()
		n.stopWatchers()
	}()
	kutil.CleanupNamespaces(ctx, n.ClientSet, fmt.Sprintf("kubernetes.io/metadata.name=%s", networkPolicyProxy))
	n.normalizeMetrics()
	for _, q := range n.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		// Divide nanoseconds by 1e6 to get milliseconds
		log.Infof("%s: %s 50th: %d 99th: %d max: %d avg: %d", n.JobConfig.Name, pq.QuantileName, pq.P50, pq.P99, pq.Max, pq.Avg)

	}
	return nil
}

func (n *netpolLatency) normalizeMetrics() {
	var latencies []float64
	var minLatencies []float64
	sLen := 0
	n.metrics.Range(func(key, value any) bool {
		sLen++
		metric := value.(netpolMetric)
		latencies = append(latencies, float64(metric.ReadyLatency))
		minLatencies = append(minLatencies, float64(metric.MinReadyLatency))
		n.normLatencies = append(n.normLatencies, metric)
		return true
	})
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = n.Uuid
		latencySummary.Timestamp = time.Now().UTC()
		latencySummary.Metadata = n.Metadata
		latencySummary.MetricName = netpolLatencyQuantilesMeasurement
		latencySummary.JobName = n.JobConfig.Name
		return latencySummary
	}
	if sLen > 0 {
		n.latencyQuantiles = append(n.latencyQuantiles, calcSummary("Ready", latencies))
		n.latencyQuantiles = append(n.latencyQuantiles, calcSummary("minReady", minLatencies))
	}
}

func (n *netpolLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}
