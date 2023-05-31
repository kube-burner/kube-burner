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
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/metrics"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	kvv1 "kubevirt.io/api/core/v1"
)

const (
	vmiLatencyMeasurement          = "vmiLatencyMeasurement"
	vmiLatencyQuantilesMeasurement = "vmiLatencyQuantilesMeasurement"
)

// vmiMetric holds both pod and vmi metrics
type vmiMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	Timestamp time.Time `json:"timestamp"`

	podCreated                time.Time
	PodCreatedLatency         int `json:"podCreatedLatency"`
	podScheduled              time.Time
	PodScheduledLatency       int `json:"podScheduledLatency"`
	podInitialized            time.Time
	PodInitializedLatency     int `json:"podInitializedLatency"`
	podContainersReady        time.Time
	PodContainersReadyLatency int `json:"podContainersReadyLatency"`
	podReady                  time.Time
	PodReadyLatency           int `json:"podReadyLatency"`

	vmiCreated           time.Time
	VMICreatedLatency    int `json:"vmiCreatedLatency"`
	vmiPending           time.Time
	VMIPendingLatency    int `json:"vmiPendingLatency"`
	vmiScheduling        time.Time
	VMISchedulingLatency int `json:"vmiSchedulingLatency"`
	vmiScheduled         time.Time
	VMIScheduledLatency  int `json:"vmiScheduledLatency"`
	vmiRunning           time.Time
	VMIRunningLatency    int `json:"vmiRunningLatency"`
	vmiReady             time.Time
	VMIReadyLatency      int `json:"vmiReadyLatency"`

	vmReady        time.Time
	VMReadyLatency int `json:"vmReadyLatency"`

	MetricName string `json:"metricName"`
	JobName    string `json:"jobName"`
	UUID       string `json:"uuid"`
	Namespace  string `json:"namespace"`
	Name       string `json:"podName"`
	NodeName   string `json:"nodeName"`
}

type vmiLatency struct {
	config           types.Measurement
	vmWatcher        *metrics.Watcher
	vmiWatcher       *metrics.Watcher
	vmiPodWatcher    *metrics.Watcher
	metrics          map[string]*vmiMetric
	latencyQuantiles []interface{}
	normLatencies    []interface{}
	mu               sync.Mutex
}

func init() {
	measurementMap["vmiLatency"] = &vmiLatency{}
}

func (p *vmiLatency) handleCreateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmID := vm.Labels["kubevirt-vm"]
	p.mu.Lock()
	if _, exists := p.metrics[vmID]; !exists {
		if strings.Contains(vm.Namespace, factory.jobConfig.Namespace) {
			p.metrics[vmID] = &vmiMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  vm.Namespace,
				Name:       vm.Name,
				MetricName: vmiLatencyMeasurement,
				UUID:       factory.uuid,
				JobName:    factory.jobConfig.Name,
			}
		}
	}
	p.mu.Unlock()
}

func (p *vmiLatency) handleUpdateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmID := vm.Labels["kubevirt-vm"]
	p.mu.Lock()
	if vmM, exists := p.metrics[vmID]; exists && vmM.vmReady.IsZero() {
		for _, c := range vm.Status.Conditions {
			if c.Status == v1.ConditionTrue {
				if c.Type == kvv1.VirtualMachineReady {
					vmM.vmReady = time.Now().UTC()
				}
			}
		}
	}
	p.mu.Unlock()
}

func (p *vmiLatency) handleCreateVMI(obj interface{}) {
	var vmID string
	vmi := obj.(*kvv1.VirtualMachineInstance)
	// in case the parent is a VM object
	if id, exists := vmi.Labels["kubevirt-vm"]; exists {
		vmID = id
	}
	// in case there is no parent
	if vmID == "" {
		vmID = string(vmi.UID)
	}
	p.mu.Lock()
	if _, exists := p.metrics[vmID]; !exists {
		if strings.Contains(vmi.Namespace, factory.jobConfig.Namespace) {
			p.metrics[vmID] = &vmiMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  vmi.Namespace,
				Name:       vmi.Name,
				MetricName: vmiLatencyMeasurement,
				UUID:       factory.uuid,
				JobName:    factory.jobConfig.Name,
			}
		}
	}
	if vmiM, exists := p.metrics[vmID]; exists {
		if vmiM.vmiCreated.IsZero() {
			vmiM.vmiCreated = time.Now().UTC()
		}
	}
	p.mu.Unlock()
}

func (p *vmiLatency) handleUpdateVMI(obj interface{}) {
	var vmID string
	vmi := obj.(*kvv1.VirtualMachineInstance)
	// in case the parent is a VM object
	if id, exists := vmi.Labels["kubevirt-vm"]; exists {
		vmID = id
	}
	// in case the parent is a VMI object
	if vmID == "" {
		vmID = string(vmi.UID)
	}
	p.mu.Lock()
	if vmiM, exists := p.metrics[vmID]; exists && vmiM.vmReady.IsZero() {
		for _, c := range vmi.Status.Conditions {
			if c.Status == v1.ConditionTrue {
				if c.Type == kvv1.VirtualMachineInstanceReady {
					vmiM.vmiReady = time.Now().UTC()
					log.Debugf("VMI %s is Ready", vmi.Name)
				}
			}
		}
		// Although the pattern of using phase is deprecated, kubevirt still strongly relies on it.
		switch vmi.Status.Phase {
		case kvv1.Pending:
			if vmiM.vmiPending.IsZero() {
				vmiM.vmiPending = time.Now().UTC()
			}
		case kvv1.Scheduling:
			if vmiM.vmiScheduling.IsZero() {
				vmiM.vmiScheduling = time.Now().UTC()
			}
		case kvv1.Scheduled:
			if vmiM.vmiScheduled.IsZero() {
				vmiM.vmiScheduled = time.Now().UTC()
			}
		case kvv1.Running:
			if vmiM.vmiRunning.IsZero() {
				vmiM.vmiRunning = time.Now().UTC()
			}
		}
	}
	p.mu.Unlock()
}

func (p *vmiLatency) handleCreateVMIPod(obj interface{}) {
	var vmID string
	pod := obj.(*v1.Pod)
	// in case the parent is a VM object
	if id, exists := pod.Labels["kubevirt-vm"]; exists {
		vmID = id
	}
	// in case the parent is a VMI object
	if id, exists := pod.Labels["kubevirt.io/created-by"]; exists && vmID == "" {
		vmID = id
	}
	// only get data from a pod that is owned by a VMI
	if vmID == "" {
		return
	}
	p.mu.Lock()
	if vmiM, exists := p.metrics[vmID]; exists {
		if vmiM.podCreated.IsZero() {
			vmiM.podCreated = time.Now().UTC()
		}
	}
	p.mu.Unlock()
}

func (p *vmiLatency) handleUpdateVMIPod(obj interface{}) {
	var vmID string
	pod := obj.(*v1.Pod)
	// in case the parent is a VM object
	if id, exists := pod.Labels["kubevirt-vm"]; exists {
		vmID = id
	}
	// in case the parent is a VMI object
	if id, exists := pod.Labels["kubevirt.io/created-by"]; exists && vmID == "" {
		vmID = id
	}
	// only get data from a pod that is owned by a VMI
	if vmID == "" {
		return
	}
	p.mu.Lock()
	if vmiM, exists := p.metrics[vmID]; exists && vmiM.podReady.IsZero() {
		for _, c := range pod.Status.Conditions {
			if c.Status == v1.ConditionTrue {
				switch c.Type {
				case v1.PodScheduled:
					if vmiM.podScheduled.IsZero() {
						vmiM.podScheduled = time.Now().UTC()
						vmiM.NodeName = pod.Spec.NodeName
					}
				case v1.PodInitialized:
					if vmiM.podInitialized.IsZero() {
						vmiM.podInitialized = time.Now().UTC()
					}
				case v1.ContainersReady:
					if vmiM.podContainersReady.IsZero() {
						vmiM.podContainersReady = time.Now().UTC()
					}
				case v1.PodReady:
					vmiM.podReady = time.Now().UTC()
				}
			}
		}
	}
	p.mu.Unlock()
}

func (p *vmiLatency) setConfig(cfg types.Measurement) error {
	p.config = cfg
	if err := p.validateConfig(); err != nil {
		return err
	}
	return nil
}

// Start starts vmiLatency measurement
func (p *vmiLatency) start(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	p.metrics = make(map[string]*vmiMetric)

	log.Infof("Creating VM latency watcher for %s", factory.jobConfig.Name)
	restClient := newRESTClientWithRegisteredKubevirtResource()
	p.vmWatcher = metrics.NewWatcher(
		restClient,
		"vmWatcher",
		"virtualmachines",
		v1.NamespaceAll,
	)
	p.vmWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: p.handleCreateVM,
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.handleUpdateVM(newObj)
		},
	})
	if err := p.vmWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("VMI Latency measurement error: %s", err)
	}

	log.Infof("Creating VMI latency watcher for %s", factory.jobConfig.Name)
	p.vmiWatcher = metrics.NewWatcher(
		restClient,
		"vmiWatcher",
		"virtualmachineinstances",
		v1.NamespaceAll,
	)
	p.vmiWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: p.handleCreateVMI,
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.handleUpdateVMI(newObj)
		},
	})
	if err := p.vmiWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("VMI Latency measurement error: %s", err)
	}

	log.Infof("Creating VMI Pod latency watcher for %s", factory.jobConfig.Name)
	p.vmiPodWatcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"podWatcher",
		"pods",
		v1.NamespaceAll,
	)
	p.vmiPodWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: p.handleCreateVMIPod,
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.handleUpdateVMIPod(newObj)
		},
	})
	if err := p.vmiPodWatcher.StartAndCacheSync(); err != nil {
		log.Errorf("VMI Pod Latency measurement error: %s", err)
	}
}

func newRESTClientWithRegisteredKubevirtResource() *rest.RESTClient {
	shallowCopy := factory.restConfig
	setConfigDefaults(shallowCopy)
	restClient, err := rest.RESTClientFor(shallowCopy)
	if err != nil {
		log.Errorf("Error creating custom rest client: %s", err)
		panic(err)
	}
	return restClient
}

func setConfigDefaults(config *rest.Config) {
	gv := kvv1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	scheme := runtime.NewScheme()
	SchemeBuilder := runtime.NewSchemeBuilder(kvv1.AddKnownTypesGenerator(kvv1.GroupVersions))
	AddToScheme := SchemeBuilder.AddToScheme
	codecs := serializer.NewCodecFactory(scheme)
	AddToScheme(scheme)
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
}

// Stop stops vmiLatency measurement
func (p *vmiLatency) stop() (int, error) {
	p.vmWatcher.StopWatcher()
	p.vmiWatcher.StopWatcher()
	p.vmiPodWatcher.StopWatcher()
	p.normalizeMetrics()
	p.calcQuantiles()
	rc := metrics.CheckThreshold(p.config.LatencyThresholds, p.latencyQuantiles)
	if kubeburnerCfg.IndexerConfig.Enabled {
		p.index()
	}
	// Reset latency slices, required in multi-job benchmarks
	p.latencyQuantiles = nil
	p.normLatencies = nil
	return rc, nil
}

// index sends metrics to the configured indexer
func (p *vmiLatency) index() {
	indexingOpts := indexers.IndexingOpts{
		JobName: factory.jobConfig.Name,
	}
	metricMap := map[string][]interface{}{
		podLatencyMeasurement:          p.normLatencies,
		podLatencyQuantilesMeasurement: p.latencyQuantiles,
	}
	for metricName, data := range metricMap {
		indexingOpts.MetricName = metricName
		log.Debugf("Indexing [%d] documents", len(data))
		resp, err := (*factory.indexer).Index(data, indexingOpts)
		if err != nil {
			log.Error(err.Error())
		} else {
			log.Info(resp)
		}
	}
}

func (p *vmiLatency) normalizeMetrics() {
	for _, m := range p.metrics {
		// If a does not reach the Ready state (this timestamp wasn't set), we skip that vmi
		if m.vmiReady.IsZero() {
			continue
		}
		m.VMReadyLatency = int(m.vmReady.Sub(m.Timestamp).Milliseconds())

		m.VMICreatedLatency = int(m.vmiCreated.Sub(m.Timestamp).Milliseconds())
		m.VMIPendingLatency = int(m.vmiPending.Sub(m.Timestamp).Milliseconds())
		m.VMISchedulingLatency = int(m.vmiScheduling.Sub(m.Timestamp).Milliseconds())
		m.VMIScheduledLatency = int(m.vmiScheduled.Sub(m.Timestamp).Milliseconds())
		m.VMIReadyLatency = int(m.vmiReady.Sub(m.Timestamp).Milliseconds())
		m.VMIRunningLatency = int(m.vmiRunning.Sub(m.Timestamp).Milliseconds())

		m.PodCreatedLatency = int(m.podCreated.Sub(m.Timestamp).Milliseconds())
		m.PodScheduledLatency = int(m.podScheduled.Sub(m.Timestamp).Milliseconds())
		m.PodInitializedLatency = int(m.podInitialized.Sub(m.Timestamp).Milliseconds())
		m.PodContainersReadyLatency = int(m.podContainersReady.Sub(m.Timestamp).Milliseconds())
		m.PodReadyLatency = int(m.podReady.Sub(m.Timestamp).Milliseconds())

		p.normLatencies = append(p.normLatencies, m)
	}
}

func (p *vmiLatency) calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[string][]int{}
	for _, normLatency := range p.normLatencies {
		if !normLatency.(*vmiMetric).vmReady.IsZero() {
			quantileMap["VM"+string(kvv1.VirtualMachineReady)] = append(quantileMap["VM"+string(kvv1.VirtualMachineReady)], normLatency.(*vmiMetric).VMReadyLatency)
			quantileMap["VMICreated"] = append(quantileMap["VMICreated"], normLatency.(*vmiMetric).VMICreatedLatency)
		}

		quantileMap["VMI"+string(kvv1.Pending)] = append(quantileMap["VMI"+string(kvv1.Pending)], normLatency.(*vmiMetric).VMIPendingLatency)
		quantileMap["VMI"+string(kvv1.Scheduling)] = append(quantileMap["VMI"+string(kvv1.Scheduling)], normLatency.(*vmiMetric).VMISchedulingLatency)
		quantileMap["VMI"+string(kvv1.Scheduled)] = append(quantileMap["VMI"+string(kvv1.Scheduled)], normLatency.(*vmiMetric).VMIScheduledLatency)
		quantileMap["VMI"+string(kvv1.VirtualMachineInstanceReady)] = append(quantileMap["VMI"+string(kvv1.VirtualMachineInstanceReady)], normLatency.(*vmiMetric).VMIReadyLatency)

		quantileMap["PodCreated"] = append(quantileMap["PodCreated"], normLatency.(*vmiMetric).PodCreatedLatency)
		quantileMap[string(v1.PodScheduled)] = append(quantileMap[string(v1.PodScheduled)], normLatency.(*vmiMetric).PodScheduledLatency)
		quantileMap["Pod"+string(v1.PodInitialized)] = append(quantileMap["Pod"+string(v1.PodInitialized)], normLatency.(*vmiMetric).PodInitializedLatency)
		quantileMap["Pod"+string(v1.ContainersReady)] = append(quantileMap["Pod"+string(v1.ContainersReady)], normLatency.(*vmiMetric).PodContainersReadyLatency)
		quantileMap["Pod"+string(v1.PodReady)] = append(quantileMap["Pod"+string(v1.PodReady)], normLatency.(*vmiMetric).PodReadyLatency)
	}
	for quantileName, v := range quantileMap {
		quantile := metrics.LatencyQuantiles{
			QuantileName: quantileName,
			UUID:         factory.uuid,
			Timestamp:    time.Now().UTC(),
			JobName:      factory.jobConfig.Name,
			MetricName:   vmiLatencyQuantilesMeasurement,
		}
		sort.Ints(v)
		length := len(v)
		if length > 1 {
			for _, q := range quantiles {
				qValue := v[int(math.Ceil(float64(length)*q))-1]
				quantile.SetQuantile(q, qValue)
			}
			quantile.Max = v[length-1]
		}
		sum := 0
		for _, n := range v {
			sum += n
		}
		quantile.Avg = int(math.Round(float64(sum) / float64(length)))
		p.latencyQuantiles = append(p.latencyQuantiles, quantile)
	}
}

func (p *vmiLatency) validateConfig() error {
	var metricFound bool
	var latencyMetrics = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range p.config.LatencyThresholds {
		if th.ConditionType == string(kvv1.Pending) ||
			th.ConditionType == string(kvv1.Scheduling) ||
			th.ConditionType == string(kvv1.Scheduled) ||
			th.ConditionType == string(kvv1.Running) ||
			th.ConditionType == string(kvv1.VirtualMachineInstanceReady) ||
			th.ConditionType == string(kvv1.Succeeded) {
			for _, lm := range latencyMetrics {
				if th.Metric == lm {
					metricFound = true
					break
				}
			}
			if !metricFound {
				return fmt.Errorf("unsupported metric %s in vmLatency measurement, supported are: %s", th.Metric, strings.Join(latencyMetrics, ", "))
			}
		} else {
			return fmt.Errorf("unsupported vm condition type in vmLatency measurement: %s", th.ConditionType)
		}
	}
	return nil
}
