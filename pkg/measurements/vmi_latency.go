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
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	UUID       string `json:"uuid"`
	Namespace  string `json:"namespace"`
	Name       string `json:"podName"`
	NodeName   string `json:"nodeName"`
	JobName    string `json:"jobName,omitempty"`
}

type vmiLatency struct {
	config           types.Measurement
	vmWatcher        *metrics.Watcher
	vmiWatcher       *metrics.Watcher
	vmiPodWatcher    *metrics.Watcher
	metrics          sync.Map
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

func init() {
	measurementMap["vmiLatency"] = &vmiLatency{}
}

func (vmi *vmiLatency) handleCreateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmID := vm.Labels["kubevirt-vm"]
	if _, exists := vmi.metrics.Load(vmID); !exists {
		if strings.Contains(vm.Namespace, factory.jobConfig.Namespace) {
			vmi.metrics.Store(vmID, &vmiMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  vm.Namespace,
				Name:       vm.Name,
				MetricName: vmiLatencyMeasurement,
				UUID:       globalCfg.UUID,
				JobName:    factory.jobConfig.Name,
			})
		}
	}
}

func (vmi *vmiLatency) handleUpdateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmID := vm.Labels["kubevirt-vm"]
	if vmM, ok := vmi.metrics.Load(vmID); ok {
		vmMetric := vmM.(*vmiMetric)
		if vmMetric.vmReady.IsZero() {
			for _, c := range vm.Status.Conditions {
				if c.Status == corev1.ConditionTrue && c.Type == kvv1.VirtualMachineReady {
					vmMetric.vmReady = time.Now().UTC()
					log.Infof("Updated VM readiness time: %s", vm.Name)
					break
				}
			}
		}
	}
}

func (vmi *vmiLatency) handleCreateVMI(obj interface{}) {
	var vmID string
	vmiObj := obj.(*kvv1.VirtualMachineInstance)
	// in case the parent is a VM object
	if id, exists := vmiObj.Labels["kubevirt-vm"]; exists {
		vmID = id
	}
	// in case there is no parent
	if vmID == "" {
		vmID = string(vmiObj.UID)
	}
	if _, exists := vmi.metrics.Load(vmID); !exists {
		if strings.Contains(vmiObj.Namespace, factory.jobConfig.Namespace) {
			vmi.metrics.Store(vmID, &vmiMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  vmiObj.Namespace,
				Name:       vmiObj.Name,
				MetricName: vmiLatencyMeasurement,
				UUID:       globalCfg.UUID,
			})
		}
	}
	if vmiM, ok := vmi.metrics.Load(vmID); ok {
		vmiMetric := vmiM.(*vmiMetric)
		if vmiMetric.vmiCreated.IsZero() {
			vmiMetric.vmiCreated = time.Now().UTC()
		}
	}
}

func (vmi *vmiLatency) handleUpdateVMI(obj interface{}) {
	var vmID string
	vmiObj := obj.(*kvv1.VirtualMachineInstance)
	// in case the parent is a VM object
	if id, exists := vmiObj.Labels["kubevirt-vm"]; exists {
		vmID = id
	}
	// in case the parent is a VMI object
	if vmID == "" {
		vmID = string(vmiObj.UID)
	}
	if vmiM, ok := vmi.metrics.Load(vmID); ok {
		vmiMetric := vmiM.(*vmiMetric)
		if vmiMetric.vmiReady.IsZero() {
			for _, c := range vmiObj.Status.Conditions {
				if c.Status == corev1.ConditionTrue && c.Type == kvv1.VirtualMachineInstanceReady {
					vmiMetric.vmiReady = time.Now().UTC()
					log.Infof("Updated VMI readiness time: %s", vmiObj.Name)
					break
				}
			}
			switch vmiObj.Status.Phase {
			case kvv1.Pending:
				if vmiMetric.vmiPending.IsZero() {
					vmiMetric.vmiPending = time.Now().UTC()
				}
			case kvv1.Scheduling:
				if vmiMetric.vmiScheduling.IsZero() {
					vmiMetric.vmiScheduling = time.Now().UTC()
				}
			case kvv1.Scheduled:
				if vmiMetric.vmiScheduled.IsZero() {
					vmiMetric.vmiScheduled = time.Now().UTC()
				}
			case kvv1.Running:
				if vmiMetric.vmiRunning.IsZero() {
					vmiMetric.vmiRunning = time.Now().UTC()
				}
			}
		}
	}
}

func (vmi *vmiLatency) handleCreateVMIPod(obj interface{}) {
	var vmID string
	pod := obj.(*corev1.Pod)
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
	if vmM, ok := vmi.metrics.Load(vmID); ok {
		vmiMetric := vmM.(*vmiMetric)
		if vmiMetric.podCreated.IsZero() {
			vmiMetric.podCreated = time.Now().UTC()
			log.Infof("Updated pod creation time for VMI: %s", pod.Name)
		}
	}
}

func (vmi *vmiLatency) handleUpdateVMIPod(obj interface{}) {
	var vmID string
	pod := obj.(*corev1.Pod)
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
	if vmM, ok := vmi.metrics.Load(vmID); ok {
		vmiMetric := vmM.(*vmiMetric)
		if vmiMetric.podReady.IsZero() {
			for _, c := range pod.Status.Conditions {
				if c.Status == corev1.ConditionTrue {
					switch c.Type {
					case corev1.PodScheduled:
						if vmiMetric.podScheduled.IsZero() {
							vmiMetric.podScheduled = time.Now().UTC()
							vmiMetric.NodeName = pod.Spec.NodeName
							log.Infof("Updated pod scheduling time for VMI: %s", pod.Name)
						}
					case corev1.PodInitialized:
						if vmiMetric.podInitialized.IsZero() {
							vmiMetric.podInitialized = time.Now().UTC()
							log.Infof("Updated pod initialization time for VMI: %s", pod.Name)
						}
					case corev1.ContainersReady:
						if vmiMetric.podContainersReady.IsZero() {
							vmiMetric.podContainersReady = time.Now().UTC()
							log.Infof("Updated pod containers ready time for VMI: %s", pod.Name)
						}
					case corev1.PodReady:
						vmiMetric.podReady = time.Now().UTC()
						log.Infof("Updated pod readiness time for VMI: %s", pod.Name)
					}
				}
			}
		}
	}
}

func (vmi *vmiLatency) setConfig(cfg types.Measurement) error {
	vmi.config = cfg
	var metricFound bool
	var latencyMetrics = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range vmi.config.LatencyThresholds {
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

// Start starts vmiLatency measurement
func (vmi *vmiLatency) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Reset latency slices, required in multi-job benchmarks
	vmi.latencyQuantiles, vmi.normLatencies = nil, nil
	vmi.metrics = sync.Map{}
	log.Infof("Creating VM latency watcher for %s", factory.jobConfig.Name)
	restClient := newRESTClientWithRegisteredKubevirtResource()
	vmi.vmWatcher = metrics.NewWatcher(
		restClient,
		"vmWatcher",
		"virtualmachines",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", globalCfg.RUNID)
		},
		nil,
	)
	vmi.vmWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: vmi.handleCreateVM,
		UpdateFunc: func(oldObj, newObj interface{}) {
			vmi.handleUpdateVM(newObj)
		},
	})
	if err := vmi.vmWatcher.StartAndCacheSync(); err != nil {
		return fmt.Errorf("VMI Latency measurement error: %s", err)
	}

	log.Infof("Creating VMI latency watcher for %s", factory.jobConfig.Name)
	vmi.vmiWatcher = metrics.NewWatcher(
		restClient,
		"vmiWatcher",
		"virtualmachineinstances",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", globalCfg.RUNID)
		},
		nil,
	)
	vmi.vmiWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: vmi.handleCreateVMI,
		UpdateFunc: func(oldObj, newObj interface{}) {
			vmi.handleUpdateVMI(newObj)
		},
	})
	if err := vmi.vmiWatcher.StartAndCacheSync(); err != nil {
		return fmt.Errorf("VMI Latency measurement error: %s", err)
	}

	log.Infof("Creating VMI Pod latency watcher for %s", factory.jobConfig.Name)
	vmi.vmiPodWatcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"podWatcher",
		"pods",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", globalCfg.RUNID)
		},
		nil,
	)
	vmi.vmiPodWatcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: vmi.handleCreateVMIPod,
		UpdateFunc: func(oldObj, newObj interface{}) {
			vmi.handleUpdateVMIPod(newObj)
		},
	})
	if err := vmi.vmiPodWatcher.StartAndCacheSync(); err != nil {
		return fmt.Errorf("VMI Pod Latency measurement error: %s", err)
	}
	return nil
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

func (vmi *vmiLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

// Stop stops vmiLatency measurement
func (vmi *vmiLatency) stop() error {
	defer func() {
		vmi.vmWatcher.StopWatcher()
		vmi.vmiWatcher.StopWatcher()
		vmi.vmiPodWatcher.StopWatcher()
	}()
	if factory.jobConfig.JobType == config.DeletionJob {
		return nil
	}
	vmi.normalizeMetrics()
	vmi.calcQuantiles()
	err := metrics.CheckThreshold(vmi.config.LatencyThresholds, vmi.latencyQuantiles)
	for _, q := range vmi.latencyQuantiles {
		vmiq := q.(metrics.LatencyQuantiles)
		// Divide nanoseconds by 1e6 to get milliseconds
		log.Infof("%s: %s 99th: %dms max: %dms avg: %dms", factory.jobConfig.Name, vmiq.QuantileName, vmiq.P99/1e6, vmiq.Max/1e6, vmiq.Avg/1e6)
	}
	return err
}

// index sends metrics to the configured indexer
func (vmi *vmiLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]interface{}{
		vmiLatencyMeasurement:          vmi.normLatencies,
		vmiLatencyQuantilesMeasurement: vmi.latencyQuantiles,
	}
	IndexLatencyMeasurement(vmi.config, jobName, metricMap, indexerList)
}

func (vmi *vmiLatency) getMetrics() *sync.Map {
	return &vmi.metrics
}

func (vmi *vmiLatency) normalizeMetrics() {
	vmi.metrics.Range(func(key, value interface{}) bool {
		m := value.(*vmiMetric)
		if m.vmiReady.IsZero() {
			return true
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
		m.JobName = factory.jobConfig.Name
		vmi.normLatencies = append(vmi.normLatencies, m)
		return true
	})
}

func (vmi *vmiLatency) calcQuantiles() {
	quantileMap := map[string][]float64{}
	for _, normLatency := range vmi.normLatencies {
		if !normLatency.(*vmiMetric).vmReady.IsZero() {
			quantileMap["VM"+string(kvv1.VirtualMachineReady)] = append(quantileMap["VM"+string(kvv1.VirtualMachineReady)], float64(normLatency.(*vmiMetric).VMReadyLatency))
			quantileMap["VMICreated"] = append(quantileMap["VMICreated"], float64(normLatency.(*vmiMetric).VMICreatedLatency))
		}

		quantileMap["VMI"+string(kvv1.Pending)] = append(quantileMap["VMI"+string(kvv1.Pending)], float64(normLatency.(*vmiMetric).VMIPendingLatency))
		quantileMap["VMI"+string(kvv1.Scheduling)] = append(quantileMap["VMI"+string(kvv1.Scheduling)], float64(normLatency.(*vmiMetric).VMISchedulingLatency))
		quantileMap["VMI"+string(kvv1.Scheduled)] = append(quantileMap["VMI"+string(kvv1.Scheduled)], float64(normLatency.(*vmiMetric).VMIScheduledLatency))
		quantileMap["VMI"+string(kvv1.VirtualMachineInstanceReady)] = append(quantileMap["VMI"+string(kvv1.VirtualMachineInstanceReady)], float64(normLatency.(*vmiMetric).VMIReadyLatency))
		quantileMap["PodCreated"] = append(quantileMap["PodCreated"], float64(normLatency.(*vmiMetric).PodCreatedLatency))
		quantileMap[string(corev1.PodScheduled)] = append(quantileMap[string(corev1.PodScheduled)], float64(normLatency.(*vmiMetric).PodScheduledLatency))
		quantileMap["Pod"+string(corev1.PodInitialized)] = append(quantileMap["Pod"+string(corev1.PodInitialized)], float64(normLatency.(*vmiMetric).PodInitializedLatency))
		quantileMap["Pod"+string(corev1.ContainersReady)] = append(quantileMap["Pod"+string(corev1.ContainersReady)], float64(normLatency.(*vmiMetric).PodContainersReadyLatency))
		quantileMap["Pod"+string(corev1.PodReady)] = append(quantileMap["Pod"+string(corev1.PodReady)], float64(normLatency.(*vmiMetric).PodReadyLatency))

	}
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = globalCfg.UUID
		latencySummary.Metadata = factory.metadata
		latencySummary.MetricName = vmiLatencyQuantilesMeasurement
		return latencySummary
	}
	for podCondition, latencies := range quantileMap {
		vmi.latencyQuantiles = append(vmi.latencyQuantiles, calcSummary(podCondition, latencies))
	}
}
