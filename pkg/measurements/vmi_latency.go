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
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	kvv1 "kubevirt.io/api/core/v1"
)

const (
	vmiLatencyMeasurement          = "vmiLatencyMeasurement"
	vmiLatencyQuantilesMeasurement = "vmiLatencyQuantilesMeasurement"
)

var (
	supportedVMIConditions map[string]struct{} = map[string]struct{}{
		"VMI" + string(kvv1.Pending):    {},
		"VMI" + string(kvv1.Scheduling): {},
		"VMI" + string(kvv1.Scheduled):  {},
		"VMI" + string(kvv1.Running):    {},
	}
)

// vmiMetric holds both pod and vmi metrics
type vmiMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	Timestamp time.Time `json:"timestamp"`

	podCreated                time.Time
	PodCreatedLatency         int64 `json:"podCreatedLatency"`
	podScheduled              time.Time
	PodScheduledLatency       int64 `json:"podScheduledLatency"`
	podInitialized            time.Time
	PodInitializedLatency     int64 `json:"podInitializedLatency"`
	podContainersReady        time.Time
	PodContainersReadyLatency int64 `json:"podContainersReadyLatency"`
	podReady                  time.Time
	PodReadyLatency           int64 `json:"podReadyLatency"`
	vmiCreated                time.Time
	VMICreatedLatency         int64 `json:"vmiCreatedLatency"`
	vmiPending                time.Time
	VMIPendingLatency         int64 `json:"vmiPendingLatency"`
	vmiScheduling             time.Time
	VMISchedulingLatency      int64 `json:"vmiSchedulingLatency"`
	vmiScheduled              time.Time
	VMIScheduledLatency       int64 `json:"vmiScheduledLatency"`
	vmiRunning                time.Time
	VMIRunningLatency         int64 `json:"vmiRunningLatency"`
	vmReady                   time.Time
	VMReadyLatency            int64       `json:"vmReadyLatency"`
	MetricName                string      `json:"metricName"`
	UUID                      string      `json:"uuid"`
	Namespace                 string      `json:"namespace"`
	PodName                   string      `json:"podName,omitempty"`
	VMName                    string      `json:"vmName,omitempty"`
	VMIName                   string      `json:"vmiName,omitempty"`
	NodeName                  string      `json:"nodeName"`
	JobName                   string      `json:"jobName,omitempty"`
	Metadata                  interface{} `json:"metadata,omitempty"`
	JobIteration              int         `json:"jobIteration"`
	Replica                   int         `json:"replica"`
}

type vmiLatency struct {
	BaseMeasurement
}

type vmiLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newVmiLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]interface{}) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedVMIConditions); err != nil {
		return nil, err
	}
	return vmiLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (vmilmf vmiLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) Measurement {
	return &vmiLatency{
		BaseMeasurement: vmilmf.NewBaseLatency(jobConfig, clientSet, restConfig, vmiLatencyMeasurement, vmiLatencyQuantilesMeasurement),
	}
}

func (vmi *vmiLatency) handleCreateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmLabels := vm.GetLabels()
	vmi.metrics.LoadOrStore(string(vm.UID), vmiMetric{
		Namespace:    vm.Namespace,
		MetricName:   vmiLatencyMeasurement,
		VMName:       vm.Name,
		JobIteration: getIntFromLabels(vmLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(vmLabels, config.KubeBurnerLabelReplica),
		Timestamp:    vm.CreationTimestamp.UTC(),
	})
}

func (vmi *vmiLatency) handleUpdateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	if vmM, ok := vmi.metrics.Load(string(vm.UID)); ok {
		vmMetric := vmM.(vmiMetric)
		if vmMetric.vmReady.IsZero() {
			for _, c := range vm.Status.Conditions {
				if c.Status == corev1.ConditionTrue && c.Type == kvv1.VirtualMachineReady {
					vmMetric.vmReady = time.Now().UTC()
					log.Debugf("VM %s is ready", vm.Name)
					break
				}
			}
		}
		vmi.metrics.Store(string(vm.UID), vmMetric)
	}
}

func (vmi *vmiLatency) handleCreateVMI(obj interface{}) {
	vmiObj := obj.(*kvv1.VirtualMachineInstance)
	now := vmiObj.CreationTimestamp.UTC()
	parentVMID := getParentVMMapID(vmiObj)
	// in case there's a parent vm
	if parentVMID != "" {
		if vmiM, ok := vmi.metrics.Load(parentVMID); ok {
			vmiMetric := vmiM.(vmiMetric)
			if vmiMetric.vmiCreated.IsZero() {
				vmiMetric.vmiCreated = now
				vmiMetric.VMIName = vmiObj.Name
				vmi.metrics.Store(parentVMID, vmiMetric)
			}
		}
	} else {
		vmiLabels := vmiObj.GetLabels()
		vmi.metrics.Store(string(vmiObj.UID), vmiMetric{
			vmiCreated:   now,
			VMIName:      vmiObj.Name,
			JobIteration: getIntFromLabels(vmiLabels, config.KubeBurnerLabelJobIteration),
			Replica:      getIntFromLabels(vmiLabels, config.KubeBurnerLabelReplica),
			Timestamp:    now, // Timestamp only needs to be set when there's not a parent VM
		})
	}
}

func (vmi *vmiLatency) handleUpdateVMI(obj interface{}) {
	vmiObj := obj.(*kvv1.VirtualMachineInstance)
	// in case the parent is a VM object
	mapID := getParentVMMapID(vmiObj)
	// otherwise use VMI UID
	if mapID == "" {
		mapID = string(vmiObj.UID)
	}
	if vmiM, ok := vmi.metrics.Load(mapID); ok {
		vmiMetric := vmiM.(vmiMetric)
		if vmiMetric.vmiRunning.IsZero() {
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
				log.Debugf("VMI %s is running", vmiObj.Name)
				vmiMetric.vmiRunning = time.Now().UTC()
			}
			vmi.metrics.Store(mapID, vmiMetric)
		}
	}
}

func (vmi *vmiLatency) handleCreateVMIPod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	vmiName, err := getParentVMIName(pod)
	if err != nil {
		log.Warn(err.Error())
		return
	}
	// Iterate over all vmi metrics to get the one with the same VMI name
	vmi.metrics.Range(func(k, v interface{}) bool {
		vmiMetric := v.(vmiMetric)
		if vmiMetric.VMIName == vmiName {
			vmiMetric.PodName = pod.Name
			vmiMetric.podCreated = time.Now().UTC()
			vmi.metrics.Store(k, vmiMetric)
		}
		return true
	})
}

func (vmi *vmiLatency) handleUpdateVMIPod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	vmiName, err := getParentVMIName(pod)
	if err != nil {
		log.Warn(err.Error())
		return
	}
	// Iterate over all vmi metrics to get the one with the same VMI name
	vmi.metrics.Range(func(k, v interface{}) bool {
		vmiMetric := v.(vmiMetric)
		if vmiMetric.VMIName == vmiName {
			if vmiMetric.podReady.IsZero() {
				for _, c := range pod.Status.Conditions {
					if c.Status == corev1.ConditionTrue {
						switch c.Type {
						case corev1.PodScheduled:
							if vmiMetric.podScheduled.IsZero() {
								vmiMetric.podScheduled = time.Now().UTC()
								vmiMetric.NodeName = pod.Spec.NodeName
							}
						case corev1.PodInitialized:
							if vmiMetric.podInitialized.IsZero() {
								vmiMetric.podInitialized = time.Now().UTC()
							}
						case corev1.ContainersReady:
							if vmiMetric.podContainersReady.IsZero() {
								vmiMetric.podContainersReady = time.Now().UTC()
							}
						case corev1.PodReady:
							log.Debugf("VMI pod %s is running", pod.Name)
							vmiMetric.podReady = time.Now().UTC()
						}
					}
				}
			}
			vmi.metrics.Store(k, vmiMetric)
		}
		return true
	})
}

// Start starts vmiLatency measurement
func (vmi *vmiLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	restClient := newRESTClientWithRegisteredKubevirtResource(vmi.RestConfig)
	vmi.startMeasurement(
		[]MeasurementWatcher{
			{
				restClient:    restClient,
				name:          "vmWatcher",
				resource:      "virtualmachines",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", vmi.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmi.handleCreateVM,
					UpdateFunc: func(oldObj, newObj interface{}) {
						vmi.handleUpdateVM(newObj)
					},
				},
			},
			{
				restClient:    restClient,
				name:          "vmiWatcher",
				resource:      "virtualmachineinstances",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", vmi.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmi.handleCreateVMI,
					UpdateFunc: func(oldObj, newObj interface{}) {
						vmi.handleUpdateVMI(newObj)
					},
				},
			},
			{
				restClient: vmi.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
				name:       "podWatcher",
				resource:   "pods",
				labelSelector: labels.Set(
					map[string]string{
						"kubevirt.io":       "virt-launcher",
						"kube-burner-runid": vmi.Runid,
					},
				).String(),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmi.handleCreateVMIPod,
					UpdateFunc: func(oldObj, newObj interface{}) {
						vmi.handleUpdateVMIPod(newObj)
					},
				},
			},
		},
	)
	return nil
}

func newRESTClientWithRegisteredKubevirtResource(restConfig *rest.Config) *rest.RESTClient {
	shallowCopy := *restConfig
	setConfigDefaults(&shallowCopy)
	restClient, err := rest.RESTClientFor(&shallowCopy)
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

func (vmi *vmiLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

// Stop stops vmiLatency measurement
func (vmi *vmiLatency) Stop() error {
	return vmi.stopMeasurement(vmi.normalizeMetrics, vmi.getLatency)
}

func (vmi *vmiLatency) normalizeMetrics() float64 {
	vmi.metrics.Range(func(key, value interface{}) bool {
		m := value.(vmiMetric)
		if m.vmiRunning.IsZero() {
			log.Tracef("VMI %v latency ignored as it did not reach Running state", m.VMIName)
			return true
		}
		m.VMReadyLatency = m.vmReady.Sub(m.Timestamp).Milliseconds()
		m.VMICreatedLatency = m.vmiCreated.Sub(m.Timestamp).Milliseconds()
		m.VMIPendingLatency = m.vmiPending.Sub(m.Timestamp).Milliseconds()
		m.VMISchedulingLatency = m.vmiScheduling.Sub(m.Timestamp).Milliseconds()
		m.VMIScheduledLatency = m.vmiScheduled.Sub(m.Timestamp).Milliseconds()
		m.VMIRunningLatency = m.vmiRunning.Sub(m.Timestamp).Milliseconds()
		m.PodCreatedLatency = m.podCreated.Sub(m.Timestamp).Milliseconds()
		m.PodScheduledLatency = m.podScheduled.Sub(m.Timestamp).Milliseconds()
		m.PodInitializedLatency = m.podInitialized.Sub(m.Timestamp).Milliseconds()
		m.PodContainersReadyLatency = m.podContainersReady.Sub(m.Timestamp).Milliseconds()
		m.PodReadyLatency = m.podReady.Sub(m.Timestamp).Milliseconds()
		m.UUID = vmi.Uuid
		m.JobName = vmi.JobConfig.Name
		m.Metadata = vmi.Metadata
		vmi.normLatencies = append(vmi.normLatencies, m)
		return true
	})
	return 0
}

func (vmi *vmiLatency) getLatency(normLatency any) map[string]float64 {
	vmiMetric := normLatency.(vmiMetric)
	return map[string]float64{
		"VM" + string(kvv1.VirtualMachineReady): float64(vmiMetric.VMReadyLatency),
		"VMICreated":                            float64(vmiMetric.VMICreatedLatency),
		"VMI" + string(kvv1.Pending):            float64(vmiMetric.VMIPendingLatency),
		"VMI" + string(kvv1.Scheduling):         float64(vmiMetric.VMISchedulingLatency),
		"VMI" + string(kvv1.Scheduled):          float64(vmiMetric.VMIScheduledLatency),
		"VMI" + string(kvv1.Running):            float64(vmiMetric.VMIRunningLatency),
		"PodCreated":                            float64(vmiMetric.PodCreatedLatency),
		"Pod" + string(corev1.PodScheduled):     float64(vmiMetric.PodScheduledLatency),
		"Pod" + string(corev1.PodInitialized):   float64(vmiMetric.PodInitializedLatency),
		"Pod" + string(corev1.ContainersReady):  float64(vmiMetric.PodContainersReadyLatency),
	}
}

// Returns the parent VM UID if there is one
// otherwise returns an empty string
func getParentVMMapID(vmiObj *kvv1.VirtualMachineInstance) string {
	for _, or := range vmiObj.OwnerReferences {
		// Check if kind is VirtualMachine
		if or.Kind == kvv1.VirtualMachineGroupVersionKind.Kind {
			return string(or.UID)
		}
	}
	return ""
}

// Returns the parent VMI UID if there is one
// otherwise returns an empty string
func getParentVMIName(podObj *corev1.Pod) (string, error) {
	for _, or := range podObj.OwnerReferences {
		// Check if kind is VirtualMachineInstance
		if or.Kind == kvv1.VirtualMachineInstanceGroupVersionKind.Kind {
			return or.Name, nil
		}
	}
	return "", fmt.Errorf("no parent VMI found for pod %s", podObj.Name)
}
