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
	exportWorkerCount             = 10
	pingAttempts                  = 10
	numDummyIfaces                = 2
	numAddressOnDummyIface        = 2
	importWorkerCount             = 10
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

type routeImport struct {
	link string
	addr string
	pod  string
}

var stopCh = make(chan struct{})

type raMetric struct {
	Timestamp       time.Time   `json:"timestamp"`
	MetricName      string      `json:"metricName"`
	UUID            string      `json:"uuid"`
	JobName         string      `json:"jobName,omitempty"`
	Name            string      `json:"routeAdvertisementName"`
	Metadata        interface{} `json:"metadata,omitempty"`
	Scenario        string      `json:"scenario,omitempty"`
	cudn            []string
	Latency         []float64 `json:"Latency,omitempty"`
	MinReadyLatency int       `json:"minReadyLatency"`
	MaxReadyLatency int       `json:"maxReadyLatency"`
	ReadyLatency    int       `json:"readyLatency"`
}

type cudnPods struct {
	cudn string
	pods []string
}

type raLatency struct {
	baseLatencyMeasurement

	metrics           sync.Map
	latencyQuantiles  []interface{}
	normLatencies     []interface{}
	cudnSubnet        map[string]cudnPods
	cudnConnTimestamp sync.Map
	routeCh           chan netlink.RouteUpdate
	doneCh            chan struct{}
	udnclientset      userdefinednetworkclientset.Interface
	routeImportChan   chan routeImport
	wg                sync.WaitGroup
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
	var cudn []string
	ra := obj.(*routeadvertisementsv1.RouteAdvertisements)
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
		cudn = append(cudn, udn.Name)
	}
	r.metrics.LoadOrStore(ra.Name, raMetric{
		Name:       ra.Name,
		Timestamp:  ra.CreationTimestamp.Time.UTC(),
		Latency:    []float64{},
		cudn:       cudn,
		MetricName: raLatencyMeasurement,
		UUID:       r.uuid,
		Metadata:   r.metadata,
		JobName:    r.jobConfig.Name,
		Scenario:   "ExportRoutes",
	})
}

func pingAddress(srcIP, destIP string) error {
	pinger, err := ping.NewPinger(destIP)
	if err != nil {
		log.Printf("Failed to create pinger for %s: %v", destIP, err)
		return err
	}

	if srcIP != "" {
		// Required for raw sockets (root access needed)
		pinger.SetPrivileged(true)
		// Bind to the specified source IP
		pinger.Source = srcIP
	}
	pinger.Count = 1
	pinger.Timeout = 1 * time.Second
	if err := pinger.Run(); err != nil {
		log.Printf("Ping to %s failed: %v", destIP, err)
		return err
	}
	return nil
}

func (r *raLatency) importWorker() {
	defer r.wg.Done()
	for {
		select {
		case mc, ok := <-r.routeImportChan:
			if !ok {
				return
			}
			// Get the netlink.Link object for the given interface name
			link, err := netlink.LinkByName(mc.link)
			if err != nil {
				log.Fatalf("Failed to get link %s: %v", mc.link, err)
			}
			// Parse IP address
			addr, err := netlink.ParseAddr(mc.addr)
			if err != nil {
				log.Fatalf("Failed to parse IP address: %v", err)
			}

			importTimestamp := time.Now().UTC()
			// Generate route by adding IP address to the interface
			if err := netlink.AddrAdd(link, addr); err != nil {
				log.Fatalf("Failed to add IP address: %v", err)
			}
			ipAddr, _, err := net.ParseCIDR(mc.addr)
			if err != nil {
				log.Fatalf("Failed to add IP address: %v", err)
			}
			ipAddrString := ipAddr.String()
			pingSuccess := false
			for i := 0; i < pingAttempts; i++ {
				if err := pingAddress(ipAddrString, mc.pod); err == nil {
					//m.latency = append(m.latency, float64(ts.Sub(m.Timestamp).Milliseconds()))
					importLatency := float64(time.Now().UTC().Sub(importTimestamp).Milliseconds())
					r.metrics.LoadOrStore(mc.addr, raMetric{
						Name:            mc.addr,
						Timestamp:       importTimestamp,
						MetricName:      raLatencyMeasurement,
						Latency:         []float64{importLatency},
						MinReadyLatency: int(importLatency),
						MaxReadyLatency: int(importLatency),
						ReadyLatency:    int(importLatency),
						UUID:            r.uuid,
						Metadata:        r.metadata,
						JobName:         r.jobConfig.Name,
						Scenario:        "ImportRoutes",
					})
					pingSuccess = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if pingSuccess == false {
				r.metrics.LoadOrStore(mc.addr, raMetric{
					Name:            mc.addr,
					Timestamp:       importTimestamp,
					MetricName:      raLatencyMeasurement,
					Latency:         []float64{-1},
					MinReadyLatency: int(-1),
					MaxReadyLatency: int(-1),
					ReadyLatency:    int(-1),
					UUID:            r.uuid,
					Metadata:        r.metadata,
					JobName:         r.jobConfig.Name,
					Scenario:        "ImportRoutes",
				})
			}

		case <-r.doneCh:
			return
		}
	}
}
func (r *raLatency) exportWorker() {
	r.wg.Done()
	for {
		select {
		case update, ok := <-r.routeCh:
			if !ok {
				return
			}
			if update.Type == unix.RTM_NEWROUTE {
				cudnpods, exists := r.cudnSubnet[update.Route.Dst.String()]
				if exists {
					for _, pod := range cudnpods.pods {
						for i := 0; i < pingAttempts; i++ {
							if err := pingAddress("", pod); err == nil {
								var existingSlice []time.Time
								val, _ := r.cudnConnTimestamp.Load(cudnpods.cudn)
								if val != nil {
									existingSlice = val.([]time.Time)
								}

								newSlice := append(existingSlice, time.Now().UTC())

								r.cudnConnTimestamp.Store(cudnpods.cudn, newSlice)
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

func (r *raLatency) deleteDummyInterface(i int) error {
	var err error
	ifaceName := fmt.Sprintf("dummy%d", i)
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("failed to find interface %s: %v", ifaceName, err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete interface %s: %v", ifaceName, err)
	}
	return err
}

func (r *raLatency) createDummyInterface(i int) error {
	var err error
	// Define the name of the dummy interface
	ifaceName := fmt.Sprintf("dummy%d", i)

	// Create a dummy link (interface)
	dummy := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Name: ifaceName,
		},
	}

	// Add the dummy interface
	if err := netlink.LinkAdd(dummy); err != nil {
		log.Fatalf("Failed to add dummy interface: %v", err)
	}

	// Bring the interface up
	if err := netlink.LinkSetUp(dummy); err != nil {
		log.Fatalf("Failed to bring up interface: %v", err)
	}

	// Verify interface exists
	_, err = netlink.LinkByName(ifaceName)
	if err != nil {
		log.Fatalf("Failed to get interface: %v", err)
	}
	return err
}

// start raLatency measurement
func (r *raLatency) start(measurementWg *sync.WaitGroup) error {
	// Reset latency slices, required in multi-job benchmarks
	if r.jobConfig.SkipIndexing {
		return nil
	}
	var err error
	var i int
	r.latencyQuantiles, r.normLatencies = nil, nil
	defer measurementWg.Done()
	r.metrics = sync.Map{}
	r.cudnSubnet = make(map[string]cudnPods)
	r.cudnConnTimestamp = sync.Map{}
	r.routeCh = make(chan netlink.RouteUpdate, 10000)
	r.doneCh = make(chan struct{})

	r.getPods()
	podsToPingDuingImport := []string{}
	for _, cpods := range r.cudnSubnet {
		podsToPingDuingImport = append(podsToPingDuingImport, cpods.pods[0])
	}
	if len(podsToPingDuingImport) == 0 {
		return nil
	}
	for i = 0; i < numDummyIfaces; i++ {
		err = r.createDummyInterface(i)
	}
	p := 0
	r.routeImportChan = make(chan routeImport, 2250)
	for i = 0; i < numAddressOnDummyIface; i++ {
		for j := 0; j < numDummyIfaces; j++ {
			if p >= len(podsToPingDuingImport) {
				p = 0
			}
			mm := routeImport{
				link: fmt.Sprintf("dummy%d", j),
				addr: fmt.Sprintf("20.%d.%d.1/24", j, i+1),
				pod:  podsToPingDuingImport[p],
			}
			r.routeImportChan <- mm
			p += 1
		}
	}
	close(r.routeImportChan)
	// Start import worker goroutines
	for i = 0; i < importWorkerCount; i++ {
		r.wg.Add(1)
		go r.importWorker()
	}

	if err = netlink.RouteSubscribe(r.routeCh, r.doneCh); err != nil {
		log.Fatalf("Failed to subscribe to route updates: %v", err)
	}

	// Start export worker goroutines
	for i = 0; i < exportWorkerCount; i++ {
		r.wg.Add(1)
		go r.exportWorker()
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

// Stop stops raLatency measurement
func (r *raLatency) stop() error {
	if r.jobConfig.SkipIndexing {
		return nil
	}
	var err error
	close(r.doneCh)
	r.wg.Wait()

	for i := 0; i < numDummyIfaces; i++ {
		err = r.deleteDummyInterface(i)
	}
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

func (r *raLatency) normalizeMetrics() bool {
	r.metrics.Range(func(key, value interface{}) bool {
		m := value.(raMetric)
		if m.Scenario == "ExportRoutes" {
			for _, udn := range m.cudn {
				val, exists := r.cudnConnTimestamp.Load(udn)
				if exists {
					udnts := val.([]time.Time)
					for _, ts := range udnts {
						m.Latency = append(m.Latency, float64(ts.Sub(m.Timestamp).Milliseconds()))
					}
				}
			}
			latencySummary := metrics.NewLatencySummary(m.Latency, m.Name)
			log.Tracef("%s: 50th: %d 95th: %d 99th: %d min: %d max: %d avg: %d\n", m.Name, latencySummary.P50, latencySummary.P95, latencySummary.P99, latencySummary.Min, latencySummary.Max, latencySummary.Avg)

			m.MinReadyLatency = int(latencySummary.Min)
			m.MaxReadyLatency = int(latencySummary.Max)
			m.ReadyLatency = int(latencySummary.P99)
		}

		r.normLatencies = append(r.normLatencies, m)
		return true

	})
	return true
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
