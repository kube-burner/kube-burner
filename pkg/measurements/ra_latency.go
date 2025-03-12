// Copyright 2020 The Kube-burner Authors.
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
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/go-ping/ping"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	routeadvertisementsv1 "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1"
	routeadvertisementsclientset "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1/apis/clientset/versioned"
	routeadvertisementsinformerfactory "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1/apis/informers/externalversions"
	userdefinednetworkclientset "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/crd/userdefinednetwork/v1/apis/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	raLatencyMeasurement          = "raLatencyMeasurement"
	raLatencyQuantilesMeasurement = "raLatencyQuantilesMeasurement"
	workerCount                   = 10
	pingAttempts                  = 10
)

// Internal struct used to marshal PodAnnotation to the pod annotation
type podAnnotation struct {
	IPs      []string   `json:"ip_addresses"`
	MAC      string     `json:"mac_address"`
	Gateways []string   `json:"gateway_ips,omitempty"`
	Routes   []podRoute `json:"routes,omitempty"`

	IP      string `json:"ip_address,omitempty"`
	Gateway string `json:"gateway_ip,omitempty"`

	TunnelID int    `json:"tunnel_id,omitempty"`
	Role     string `json:"role,omitempty"`
}

// Internal struct used to marshal PodRoute to the pod annotation
type podRoute struct {
	Dest    string `json:"dest"`
	NextHop string `json:"nextHop"`
}

var stopCh = make(chan struct{})

type raMetric struct {
	Timestamp       time.Time     `json:"timestamp"`
	MetricName      string        `json:"metricName"`
	UUID            string        `json:"uuid"`
	JobName         string        `json:"jobName,omitempty"`
	Name            string        `json:"podName"`
	Metadata        interface{}   `json:"metadata,omitempty"`
	MinReadyLatency time.Duration `json:"minReady"`
	MaxReadyLatency time.Duration `json:"minReady"`
	ReadyLatency    time.Duration `json:"minReady"`
}

type cudnPods struct {
	cudn string
	pods []string
}

type raCudnConn struct {
	name        string
	createdTime time.Time
	latency     []float64
	cudn        []string
}

type raLatency struct {
	baseLatencyMeasurement

	metrics           sync.Map
	latencyQuantiles  []interface{}
	normLatencies     []interface{}
	cudnSubnet        map[string]cudnPods
	cudnConnTimestamp map[string][]time.Time
	raCudnConns       []raCudnConn
	routeCh           chan netlink.RouteUpdate
	doneCh            chan struct{}
	udnclientset      userdefinednetworkclientset.Interface
}

type raLatencyMeasurementFactory struct {
	baseLatencyMeasurementFactory
}

func newRaLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]interface{}) (measurementFactory, error) {
	return raLatencyMeasurementFactory{
		baseLatencyMeasurementFactory: newBaseLatencyMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (plmf raLatencyMeasurementFactory) newMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) measurement {
	return &raLatency{
		baseLatencyMeasurement: plmf.newBaseLatency(jobConfig, clientSet, restConfig),
	}
}

func (r *raLatency) getPods() error {
	listOptions := metav1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%s", r.uuid)}
	nsList, err := r.clientSet.CoreV1().Namespaces().List(context.TODO(), listOptions)
	if err != nil {
		log.Errorf("Error listing namespaces: %v", err.Error())
		return err
	}
	for _, ns := range nsList.Items {
		podList, err := r.clientSet.CoreV1().Pods(ns.Name).List(context.TODO(), listOptions)
		if err != nil {
			log.Errorf("Error listing pods in %s: %v", ns, err)
			return err
		}
		for _, pod := range podList.Items {
			podNetworks := make(map[string]podAnnotation)
			ovnAnnotation, ok := pod.Annotations["k8s.ovn.org/pod-networks"]
			if ok {
				if err := json.Unmarshal([]byte(ovnAnnotation), &podNetworks); err != nil {
					log.Debugf("failed to unmarshal ovn pod annotation  %v", err)
					continue
				}
				for pnet, val := range podNetworks {
					if pnet != "default" {
						var udn string
						parts := strings.Split(pnet, "/")
						if len(parts) == 2 {
							_, udn = parts[0], parts[1]
						} else {
							log.Debugf("Invalid input format")
							continue
						}
						ipAddr, subnet, err := net.ParseCIDR(val.IP)
						if err != nil {
							log.Debugf("Unable to get CIDR for IP")
							continue
						}
						subnetString := subnet.String()
						ipAddrString := ipAddr.String()
						cudnpods, exists := r.cudnSubnet[subnetString]
						if exists {
							cudnpods.pods = append(cudnpods.pods, ipAddrString)
							r.cudnSubnet[subnetString] = cudnpods
						} else {
							r.cudnSubnet[subnetString] = cudnPods{
								cudn: udn,
								pods: []string{ipAddrString},
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func (r *raLatency) handleAdd(obj interface{}) {
	ra := obj.(*routeadvertisementsv1.RouteAdvertisements)
	newRaCudnConn := raCudnConn{
		name:        ra.Name,
		createdTime: ra.CreationTimestamp.Time.UTC(),
		latency:     []float64{},
		cudn:        []string{},
	}
	listOptions := metav1.ListOptions{}
	selector, err := metav1.LabelSelectorAsSelector(&ra.Spec.NetworkSelector)
	if err != nil {
		log.Fatal(err)
	}
	listOptions.LabelSelector = selector.String()

	udns, err := r.udnclientset.K8sV1().ClusterUserDefinedNetworks().List(context.Background(), listOptions)
	if err != nil {
		log.Errorf("Error listing udns: %v", err.Error())
		return
	}
	for _, udn := range udns.Items {
		newRaCudnConn.cudn = append(newRaCudnConn.cudn, udn.Name)
	}
	r.raCudnConns = append(r.raCudnConns, newRaCudnConn)
}

func pingAddress(ip string) error {
	pinger, err := ping.NewPinger(ip)
	if err != nil {
		log.Printf("Failed to create pinger for %s: %v", ip, err)
		return err
	}
	pinger.Count = 1
	pinger.Timeout = 1 * time.Second
	if err := pinger.Run(); err != nil {
		log.Printf("Ping to %s failed: %v", ip, err)
		return err
	}
	return nil
}

func (r *raLatency) routeWorker() {
	for {
		select {
		case update, ok := <-r.routeCh:
			if !ok {
				log.Debugf("Route channel closed, worker exiting...")
				return
			}
			if update.Type == unix.RTM_NEWROUTE {
				cudnpods, exists := r.cudnSubnet[update.Route.Dst.String()]
				if exists {
					for _, pod := range cudnpods.pods {
						log.Debugf("New route added: %s, Pinging: %s", update.Route.Dst.String(), pod)
						for i := 0; i < pingAttempts; i++ {
							if err := pingAddress(pod); err == nil {
								cudnConnTs, exists := r.cudnConnTimestamp[cudnpods.cudn]
								if exists {
									cudnConnTs = append(cudnConnTs, time.Now().UTC())
									r.cudnConnTimestamp[cudnpods.cudn] = cudnConnTs
								} else {
									r.cudnConnTimestamp[cudnpods.cudn] = []time.Time{time.Now().UTC()}

								}
								log.Debugf("Ping to %s is succesful", pod)
								break
							}
							time.Sleep(100 * time.Millisecond)
						}
					}
				}
			}
		case <-r.doneCh:
			return
		}
	}
}

// start raLatency measurement
func (r *raLatency) start(measurementWg *sync.WaitGroup) error {
	// Reset latency slices, required in multi-job benchmarks
	r.latencyQuantiles, r.normLatencies = nil, nil
	defer measurementWg.Done()
	r.metrics = sync.Map{}
	r.cudnSubnet = make(map[string]cudnPods)
	r.cudnConnTimestamp = make(map[string][]time.Time)
	r.raCudnConns = []raCudnConn{}
	r.routeCh = make(chan netlink.RouteUpdate, 10000)
	r.doneCh = make(chan struct{})

	r.getPods()
	if err := netlink.RouteSubscribe(r.routeCh, r.doneCh); err != nil {
		log.Fatalf("Failed to subscribe to route updates: %v", err)
	}

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		go r.routeWorker()
	}

	log.Infof("Creating Router Advertisement latency watcher for %s", r.jobConfig.Name)
	routeAdvertisementsClientset, err := routeadvertisementsclientset.NewForConfig(r.restConfig)
	if err != nil {
		panic(err)
	}
	udnclientset, err := userdefinednetworkclientset.NewForConfig(r.restConfig)
	if err != nil {
		panic(err)
	}
	r.udnclientset = udnclientset
	raFactory := routeadvertisementsinformerfactory.NewSharedInformerFactory(routeAdvertisementsClientset, 30*time.Second)
	raInformer := raFactory.K8s().V1().RouteAdvertisements().Informer()
	raInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: r.handleAdd,
	})
	raFactory.Start(stopCh)
	raFactory.WaitForCacheSync(stopCh)
	return nil
}

func (r *raLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

func (r *raLatency) processLatency() {
	for _, raCudnConn := range r.raCudnConns {
		for _, udn := range raCudnConn.cudn {
			udnts, exists := r.cudnConnTimestamp[udn]
			if exists {
				for _, ts := range udnts {
					raCudnConn.latency = append(raCudnConn.latency, float64(ts.Sub(raCudnConn.createdTime).Milliseconds()))
				}
			}
		}
		name := raCudnConn.name
		latencySummary := metrics.NewLatencySummary(raCudnConn.latency, name)
		log.Debugf("RA %s latency slice %v\n", name, raCudnConn.latency)
		log.Debugf("%s: 50th: %d 95th: %d 99th: %d min: %d max: %d avg: %d\n", name, latencySummary.P50, latencySummary.P95, latencySummary.P99, latencySummary.Min, latencySummary.Max, latencySummary.Avg)

		r.metrics.Store(name, raMetric{
			Name:            name,
			Timestamp:       raCudnConn.createdTime,
			MetricName:      raLatencyMeasurement,
			MinReadyLatency: time.Duration(latencySummary.Min),
			MaxReadyLatency: time.Duration(latencySummary.Max),
			ReadyLatency:    time.Duration(latencySummary.P99),
			UUID:            r.uuid,
			Metadata:        r.metadata,
			JobName:         r.jobConfig.Name,
		})
	}
}

// Stop stops raLatency measurement
func (r *raLatency) stop() error {
	var err error
	close(r.doneCh)
	r.processLatency()
	r.normalizeMetrics()
	r.calcQuantiles()
	if len(r.config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(r.config.LatencyThresholds, r.latencyQuantiles)
	}
	for _, q := range r.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %v 99th: %v max: %v avg: %v", r.jobConfig.Name, pq.QuantileName, pq.P99, pq.Max, pq.Avg)
	}
	return err
}

// index sends metrics to the configured indexer
func (r *raLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]interface{}{
		raLatencyMeasurement:          r.normLatencies,
		raLatencyQuantilesMeasurement: r.latencyQuantiles,
	}
	IndexLatencyMeasurement(r.config, jobName, metricMap, indexerList)
}

func (r *raLatency) getMetrics() *sync.Map {
	return &r.metrics
}

func (r *raLatency) normalizeMetrics() {
	r.metrics.Range(func(key, value interface{}) bool {
		m := value.(raMetric)
		r.normLatencies = append(r.normLatencies, m)
		return true
	})
}

func (r *raLatency) calcQuantiles() {
	getLatency := func(normLatency interface{}) map[string]float64 {
		raMetric := normLatency.(raMetric)
		return map[string]float64{
			"MinReadyLatency": float64(raMetric.MinReadyLatency),
			"MaxReadyLatency": float64(raMetric.MaxReadyLatency),
			"ReadyLatency":    float64(raMetric.ReadyLatency),
		}
	}
	r.latencyQuantiles = calculateQuantiles(r.uuid, r.jobConfig.Name, r.metadata, r.normLatencies, getLatency, raLatencyQuantilesMeasurement)
}
