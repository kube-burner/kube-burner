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
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	kconfig "github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	kutil "github.com/kube-burner/kube-burner/pkg/util"
	"github.com/montanaflynn/stats"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	lcorev1 "k8s.io/client-go/listers/core/v1"
	lnetv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubectl/pkg/scheme"
)

var nsPodAddresses = make(map[string]map[string][]string)
var serverPingTestPort int32 = 8080
var proxyRoute string

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

const (
	netpolLatencyMeasurement          = "netpolLatencyMeasurement"
	netpolLatencyQuantilesMeasurement = "netpolLatencyQuantilesMeasurement"
)

type netpolLatency struct {
	config           types.Measurement
	netpolWatcher    *metrics.Watcher
	podWatcher       *metrics.Watcher
	podLister        lcorev1.PodLister
	netpolLister     lnetv1.NetworkPolicyLister
	metrics          sync.Map
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

type netpolMetric struct {
	Timestamp    time.Time     `json:"timestamp"`
	ReadyLatency time.Duration `json:"ready"`
	MetricName   string        `json:"metricName"`
	UUID         string        `json:"uuid"`
	Namespace    string        `json:"namespace"`
	Name         string        `json:"netpol"`
	Metadata     interface{}   `json:"metadata,omitempty"`
	JobName      string        `json:"jobName,omitempty"`
}

func isEmpty(raw []byte) bool {
	return strings.TrimSpace(string(raw)) == ""
}

func yamlToUnstructured(fileName string, y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvk, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatalf("Error decoding YAML (%s): %s", fileName, err)
	}
	return o, gvk
}

func prepareTemplate(original []byte) ([]byte, error) {
	// Removing all placeholders from template.
	// This needs to be done due to placeholders not being valid yaml
	if isEmpty(original) {
		return nil, fmt.Errorf("template is empty")
	}
	r, err := regexp.Compile(`\{\{.*\}\}`)
	if err != nil {
		return nil, fmt.Errorf("regexp creation error: %v", err)
	}
	original = r.ReplaceAll(original, []byte{})
	return original, nil
}

func init() {
	measurementMap["netpolLatency"] = &netpolLatency{
		metrics: sync.Map{},
	}
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

func addPodsByLabel(ns string, ps *metav1.LabelSelector) []string {
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
		pods, err := factory.clientSet.CoreV1().Pods(ns).List(context.TODO(), listOptions)
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
func (n *netpolLatency) handleCreateNetpol(obj interface{}) {
	netpol := obj.(*networkingv1.NetworkPolicy)
	npCreationTime[netpol.Name] = netpol.CreationTimestamp.Time.UTC()
}

func (n *netpolLatency) setConfig(cfg types.Measurement) error {
	n.config = cfg
	return nil
}

// Render the network policy from the object template using iteration details as input
func getNetworkPolicy(iteration int, replica int, obj kconfig.Object, objectSpec []byte) *networkingv1.NetworkPolicy {

	templateData := map[string]interface{}{
		"JobName":   factory.jobConfig.Name,
		"Iteration": strconv.Itoa(iteration),
		"UUID":      globalCfg.UUID,
		"Replica":   strconv.Itoa(replica),
	}
	for k, v := range obj.InputVars {
		templateData[k] = v
	}
	renderedObj, err := kutil.RenderTemplate(objectSpec, templateData, kutil.MissingKeyError)
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
func prepareConnections() {
	// Reset latency slices, required in multi-job benchmarks
	for _, obj := range factory.jobConfig.Objects {
		cleanTemplate, err := readTemplate(obj)
		if err != nil {
			log.Fatalf("Error in readTemplate %s: %s", obj.ObjectTemplate, err)
		}
		if getObjectType(obj, cleanTemplate) != "NetworkPolicy" {
			continue
		}
		for i := 0; i < factory.jobConfig.JobIterations; i++ {
			for r := 1; r <= obj.Replicas; r++ {
				networkPolicy := getNetworkPolicy(i, r, obj, cleanTemplate)
				nsIndex := i / factory.jobConfig.IterationsPerNamespace
				namespace := fmt.Sprintf("%s-%d", factory.jobConfig.Namespace, nsIndex)
				localPods := addPodsByLabel(namespace, &networkPolicy.Spec.PodSelector)
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
						for _, namepsace := range namespaces {
							remoteAddrs := addPodsByLabel(namepsace, from.PodSelector)
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
											var netpolExists bool
											// check if network policy doesn't exist in the addrReuse
											for _, v := range addrReuse[ar] {
												if v == networkPolicy.Name {
													netpolExists = true
													break
												}
											}
											if !netpolExists {
												addrReuse[ar] = append(addrReuse[ar], networkPolicy.Name)
											}
										}
									}
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

	url := fmt.Sprintf("http://%s/initiate", proxyRoute)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Debugf("Connections sent successfully")
	}
	waitForCondition(fmt.Sprintf("http://%s/checkConnectionsStatus", proxyRoute))
}

// Periodically check if proxy pod finished communicataion with all client pods,
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
				fmt.Println("Error decoding response:", err)
				return
			}
			if response.Result {
				return
			}
		}
	}
}

// Get results from proxy pod and parse them.
func processResults(n *netpolLatency) {
	serverURL := fmt.Sprintf("http://%s", proxyRoute)
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
				npResults[netpolName] = append(npResults[netpolName], connLatency)
			}
		}
	}
	resp.Body.Close()
	for name, npl := range npResults {
		val, _ := stats.Percentile(npl, 50)
		p50 := int(val)
		val, _ = stats.Percentile(npl, 95)
		p95 := int(val)
		val, _ = stats.Percentile(npl, 99)
		p99 := int(val)
		val, _ = stats.Max(npl)
		maxVal := int(val)
		val, _ = stats.Mean(npl)
		avgVal := int(val)
		val, _ = stats.Min(npl)
		minVal := int(val)
		log.Tracef("%s: 50th: %d 95th: %d 99th: %d min: %d max: %d avg: %d\n", name, p50, p95, p99, minVal, maxVal, avgVal)

		// Use minVal for reporting as ping test tool might have delayed initiating ping tests from some remote addresses.
		// This can happen when remote pod was busy pinging other pods before trying our network policy pod
		n.metrics.Store(name, netpolMetric{
			Name:         name,
			Timestamp:    npCreationTime[name],
			MetricName:   netpolLatencyMeasurement,
			ReadyLatency: time.Duration(minVal),
			UUID:         globalCfg.UUID,
			Metadata:     factory.metadata,
			JobName:      factory.jobConfig.Name,
		})
	}
}

// Read network policy object template
func readTemplate(o kconfig.Object) ([]byte, error) {
	var err error
	var f io.Reader

	e := embed.FS{}
	if embedFS == e {
		f, err = kutil.ReadConfig(o.ObjectTemplate)
	} else {
		objectTemplate := path.Join(embedFSDir, o.ObjectTemplate)
		f, err = kutil.ReadEmbedConfig(embedFS, objectTemplate)
	}
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
	cleanTemplate, err := prepareTemplate(t)
	if err != nil {
		log.Fatalf("Error preparing template %s: %s", o.ObjectTemplate, err)
	}
	uns := &unstructured.Unstructured{}
	yamlToUnstructured(o.ObjectTemplate, cleanTemplate, uns)
	return uns.GetKind()
}

// Start network policy measurement
func (n *netpolLatency) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Use proxyRoute to communicate with proxy pod
	proxyRoute = n.config.Config["proxyRoute"]
	// Skip latency measurement for 1st job which creates only pods
	if value, ok := factory.jobConfig.NamespaceLabels["kube-burner.io/skip-networkpolicy-latency"]; ok {
		if value == "true" {
			log.Debugf("Discarding network policy latency measurement for the job %v", factory.jobConfig.Name)
			return nil
		}
	}
	// Parse network policy template for each iteration and prepare connections for client pods
	prepareConnections()
	// send connection information to proxy to deliver to client pods
	sendConnections()
	n.latencyQuantiles, n.normLatencies = nil, nil

	// Create watchers to record network policy creation timestamp
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
	// Pod watcher is needed to check if the pods are in active state or not
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
	// Skip latency measurement for 1st job which creates only pods
	if value, ok := factory.jobConfig.NamespaceLabels["kube-burner.io/skip-networkpolicy-latency"]; ok {
		if value == "true" {
			return nil
		}
	}
	processResults(n)
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
		log.Infof("%s: %s 50th: %d 99th: %d max: %d avg: %d", factory.jobConfig.Name, pq.QuantileName, pq.P50, pq.P99, pq.Max, pq.Avg)

	}
	return nil
}

func (n *netpolLatency) getMetrics() *sync.Map {
	return &n.metrics
}

func (n *netpolLatency) normalizeMetrics() {
	var latencies []float64
	sLen := 0
	n.metrics.Range(func(key, value interface{}) bool {
		sLen++
		metric := value.(netpolMetric)
		latencies = append(latencies, float64(metric.ReadyLatency))
		n.normLatencies = append(n.normLatencies, metric)
		return true
	})
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = globalCfg.UUID
		latencySummary.Timestamp = time.Now().UTC()
		latencySummary.Metadata = factory.metadata
		latencySummary.MetricName = netpolLatencyQuantilesMeasurement
		latencySummary.JobName = factory.jobConfig.Name
		return latencySummary
	}
	if sLen > 0 {
		n.latencyQuantiles = append(n.latencyQuantiles, calcSummary("Ready", latencies))
	}
}

func (n *netpolLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]interface{}{
		netpolLatencyMeasurement:          n.normLatencies,
		netpolLatencyQuantilesMeasurement: n.latencyQuantiles,
	}
	IndexLatencyMeasurement(n.config, jobName, metricMap, indexerList)
}

func (n *netpolLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}
