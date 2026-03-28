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
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
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
		"VM" + string(kvv1.VirtualMachineReady): {},
		"VMICreated":                            {},
		"VMI" + string(kvv1.Pending):            {},
		"VMI" + string(kvv1.Scheduling):         {},
		"VMI" + string(kvv1.Scheduled):          {},
		"VMI" + string(kvv1.Running):            {},
		"PodCreated":                            {},
		"Pod" + string(corev1.PodScheduled):     {},
		"Pod" + string(corev1.PodInitialized):   {},
		"Pod" + string(corev1.ContainersReady):  {},
		"Pod" + string(corev1.PodReady):         {},
	}
	supportedVMILatencyJobTypes = map[config.JobType]struct{}{
		config.CreationJob: {},
		config.KubeVirtJob: {},
		config.DeletionJob: {},
	}
)

type vmiLatencyLabels struct {
	PodName      string `json:"podName,omitempty"`
	VMName       string `json:"vmName,omitempty"`
	VMIName      string `json:"vmiName,omitempty"`
	NodeName     string `json:"nodeName"`
	Replica      int    `json:"replica"`
	JobIteration int    `json:"jobIteration"`
	Namespace    string `json:"namespace"`
}

// vmiMetric holds both pod and vmi metrics
type vmiMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	metrics.LatencyDocument
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
	VMReadyLatency            int64 `json:"vmReadyLatency"`
	vmiLatencyLabels          vmiLatencyLabels
}

type vmiLatency struct {
	BaseMeasurement
}

type vmiLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newVmiLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any, labelSelector string) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedVMIConditions); err != nil {
		return nil, err
	}
	return vmiLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata, labelSelector),
	}, nil
}

func (vmilmf vmiLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &vmiLatency{
		BaseMeasurement: vmilmf.NewBaseLatency(jobConfig, clientSet, restConfig, vmiLatencyMeasurement, vmiLatencyQuantilesMeasurement, embedCfg),
	}
}

func (vmi *vmiLatency) handleCreateVM(obj any) {
	vm, err := util.ConvertAnyToTyped[kvv1.VirtualMachine](obj)
	if err != nil {
		log.Errorf("failed to convert to VirtualMachine: %v", err)
		return
	}
	vmLabels := vm.GetLabels()
	vmi.Metrics.LoadOrStore(string(vm.UID), vmiMetric{
		LatencyDocument: metrics.LatencyDocument{
			MetricName: vmiLatencyMeasurement,
			Timestamp:  vm.CreationTimestamp.UTC(),
		},
		vmiLatencyLabels: vmiLatencyLabels{
			VMName:       vm.Name,
			Replica:      getIntFromLabels(vmLabels, config.KubeBurnerLabelReplica),
			Namespace:    vm.Namespace,
			JobIteration: getIntFromLabels(vmLabels, config.KubeBurnerLabelJobIteration),
		},
	})
}

func (vmi *vmiLatency) handleUpdateVM(obj any) {
	vm, err := util.ConvertAnyToTyped[kvv1.VirtualMachine](obj)
	if err != nil {
		log.Errorf("failed to convert to VirtualMachine: %v", err)
		return
	}
	if vmM, ok := vmi.Metrics.Load(string(vm.UID)); ok {
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
		vmi.Metrics.Store(string(vm.UID), vmMetric)
	}
}

func (vmi *vmiLatency) handleCreateVMI(obj any) {
	vmiObj, err := util.ConvertAnyToTyped[kvv1.VirtualMachineInstance](obj)
	if err != nil {
		log.Errorf("failed to convert to VirtualMachineInstance: %v", err)
		return
	}
	now := vmiObj.CreationTimestamp.UTC()
	parentVMID := getParentVMMapID(vmiObj)
	// in case there's a parent vm
	if parentVMID != "" {
		if vmiM, ok := vmi.Metrics.Load(parentVMID); ok {
			vmiMetric := vmiM.(vmiMetric)
			if vmiMetric.vmiCreated.IsZero() {
				vmiMetric.vmiCreated = now
				vmiMetric.vmiLatencyLabels.VMIName = vmiObj.Name
				vmi.Metrics.Store(parentVMID, vmiMetric)
			}
		}
	} else {
		vmiLabels := vmiObj.GetLabels()
		vmi.Metrics.Store(string(vmiObj.UID), vmiMetric{
			LatencyDocument: metrics.LatencyDocument{
				Timestamp: now, // Timestamp only needs to be set when there's not a parent VM
			},
			vmiLatencyLabels: vmiLatencyLabels{
				VMIName:      vmiObj.Name,
				JobIteration: getIntFromLabels(vmiLabels, config.KubeBurnerLabelJobIteration),
				Replica:      getIntFromLabels(vmiLabels, config.KubeBurnerLabelReplica),
			},
			vmiCreated: now,
		})
	}
}

func (vmi *vmiLatency) handleUpdateVMI(obj any) {
	vmiObj, err := util.ConvertAnyToTyped[kvv1.VirtualMachineInstance](obj)
	if err != nil {
		log.Errorf("failed to convert to VirtualMachineInstance: %v", err)
		return
	}
	// in case the parent is a VM object
	mapID := getParentVMMapID(vmiObj)
	// otherwise use VMI UID
	if mapID == "" {
		mapID = string(vmiObj.UID)
	}
	if vmiM, ok := vmi.Metrics.Load(mapID); ok {
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
			vmi.Metrics.Store(mapID, vmiMetric)
		}
	}
}

func (vmi *vmiLatency) handleCreateVMIPod(obj any) {
	pod, err := util.ConvertAnyToTyped[corev1.Pod](obj)
	if err != nil {
		log.Errorf("failed to convert to Pod: %v", err)
		return
	}
	vmiName, err := getParentVMIName(pod)
	if err != nil {
		log.Warn(err.Error())
		return
	}
	// Iterate over all vmi metrics to get the one with the same VMI name
	vmi.Metrics.Range(func(k, v any) bool {
		vmiMetric := v.(vmiMetric)
		if vmiMetric.vmiLatencyLabels.VMIName == vmiName {
			vmiMetric.vmiLatencyLabels.PodName = pod.Name
			vmiMetric.podCreated = time.Now().UTC()
			vmi.Metrics.Store(k, vmiMetric)
		}
		return true
	})
}

func (vmi *vmiLatency) handleUpdateVMIPod(obj any) {
	pod, err := util.ConvertAnyToTyped[corev1.Pod](obj)
	if err != nil {
		log.Errorf("failed to convert to Pod: %v", err)
		return
	}
	vmiName, err := getParentVMIName(pod)
	if err != nil {
		log.Warn(err.Error())
		return
	}
	// Iterate over all vmi metrics to get the one with the same VMI name
	vmi.Metrics.Range(func(k, v any) bool {
		vmiMetric := v.(vmiMetric)
		if vmiMetric.vmiLatencyLabels.VMIName == vmiName {
			if vmiMetric.podReady.IsZero() {
				for _, c := range pod.Status.Conditions {
					if c.Status == corev1.ConditionTrue {
						switch c.Type {
						case corev1.PodScheduled:
							if vmiMetric.podScheduled.IsZero() {
								vmiMetric.podScheduled = time.Now().UTC()
								vmiMetric.vmiLatencyLabels.NodeName = pod.Spec.NodeName
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
			vmi.Metrics.Store(k, vmiMetric)
		}
		return true
	})
}

// Start starts vmiLatency measurement
func (vmi *vmiLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	vmgvr, err := util.ResourceToGVR(vmi.RestConfig, "VirtualMachine", "kubevirt.io/v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "VirtualMachine", err)
	}
	vmigvr, err := util.ResourceToGVR(vmi.RestConfig, "VirtualMachineInstance", "kubevirt.io/v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "VirtualMachineInstance", err)
	}
	pgvr, err := util.ResourceToGVR(vmi.RestConfig, "Pod", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "Pod", err)
	}
	vmi.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(vmi.RestConfig),
				name:          "vmWatcher",
				resource:      vmgvr,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmi.handleCreateVM,
					UpdateFunc: func(oldObj, newObj any) {
						vmi.handleUpdateVM(newObj)
					},
				},
				transform: virtualMachineTransformFunc(),
			},
			{
				dynamicClient: dynamic.NewForConfigOrDie(vmi.RestConfig),
				name:          "vmiWatcher",
				resource:      vmigvr,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmi.handleCreateVMI,
					UpdateFunc: func(oldObj, newObj any) {
						vmi.handleUpdateVMI(newObj)
					},
				},
				transform: virtualMachineInstanceTransformFunc(),
			},
			{
				dynamicClient: dynamic.NewForConfigOrDie(vmi.RestConfig),
				name:          "podWatcher",
				resource:      pgvr,
				labelSelector: labels.Set{
					"kubevirt.io": "virt-launcher",
				}.String(),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmi.handleCreateVMIPod,
					UpdateFunc: func(oldObj, newObj any) {
						vmi.handleUpdateVMIPod(newObj)
					},
				},
				transform: PodTransformFunc(),
			},
		},
	)
	return nil
}

func (vmi *vmiLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

// Stop stops vmiLatency measurement
func (vmi *vmiLatency) Stop() error {
	return vmi.StopMeasurement(vmi.normalizeMetrics, vmi.getLatency)
}

func (vmi *vmiLatency) normalizeMetrics() float64 {
	totalVMIs := 0
	erroredVMIs := 0

	vmi.Metrics.Range(func(key, value any) bool {
		m := value.(vmiMetric)
		if m.vmiRunning.IsZero() {
			log.Tracef("VMI %v latency ignored as it did not reach Running state", m.vmiLatencyLabels.VMIName)
			return true
		}
		if m.Timestamp.IsZero() {
			log.Tracef("VMI %v latency ignored as timestamp is not set", m.vmiLatencyLabels.VMIName)
			return true
		}
		errorFlag := 0

		computeLatency := func(timestamp time.Time, latencyName string) (int64, bool) {
			if timestamp.IsZero() {
				// Some phases may never be observed for objects picked up after watcher startup (e.g. delete jobs).
				return 0, false
			}
			latency := timestamp.Sub(m.Timestamp).Milliseconds()
			if latency < 0 {
				log.Tracef("%s for VMI %v falling under negative case. So explicitly setting it to 0", latencyName, m.vmiLatencyLabels.VMIName)
				errorFlag = 1
				return 0, true
			}
			return latency, true
		}

		baseLabels := vmiLatencyLabels{
			PodName:      m.vmiLatencyLabels.PodName,
			VMName:       m.vmiLatencyLabels.VMName,
			VMIName:      m.vmiLatencyLabels.VMIName,
			NodeName:     m.vmiLatencyLabels.NodeName,
			Replica:      m.vmiLatencyLabels.Replica,
			JobIteration: m.vmiLatencyLabels.JobIteration,
			Namespace:    m.vmiLatencyLabels.Namespace,
		}
		makeDoc := func(condition string, valueMs int64) metrics.LatencyDocument {
			var lbls map[string]string
			b, _ := json.Marshal(baseLabels)
			_ = json.Unmarshal(b, &lbls)
			lbls["condition"] = condition
			return metrics.LatencyDocument{
				Timestamp:  m.Timestamp,
				MetricName: vmiLatencyMeasurement,
				UUID:       vmi.Uuid,
				JobName:    vmi.JobConfig.Name,
				Metadata:   vmi.Metadata,
				Labels:     lbls,
				Value:      float64(valueMs),
			}
		}
		if latency, ok := computeLatency(m.vmReady, "VMReadyLatency"); ok {
			m.VMReadyLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("VM"+string(kvv1.VirtualMachineReady), m.VMReadyLatency))
		}
		if latency, ok := computeLatency(m.vmiCreated, "VMICreatedLatency"); ok {
			m.VMICreatedLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("VMICreated", m.VMICreatedLatency))
		}
		if latency, ok := computeLatency(m.vmiPending, "VMIPendingLatency"); ok {
			m.VMIPendingLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("VMI"+string(kvv1.Pending), m.VMIPendingLatency))
		}
		if latency, ok := computeLatency(m.vmiScheduling, "VMISchedulingLatency"); ok {
			m.VMISchedulingLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("VMI"+string(kvv1.Scheduling), m.VMISchedulingLatency))
		}
		if latency, ok := computeLatency(m.vmiScheduled, "VMIScheduledLatency"); ok {
			m.VMIScheduledLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("VMI"+string(kvv1.Scheduled), m.VMIScheduledLatency))
		}
		if latency, ok := computeLatency(m.vmiRunning, "VMIRunningLatency"); ok {
			m.VMIRunningLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("VMI"+string(kvv1.Running), m.VMIRunningLatency))
		}
		if latency, ok := computeLatency(m.podCreated, "PodCreatedLatency"); ok {
			m.PodCreatedLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("PodCreated", m.PodCreatedLatency))
		}
		if latency, ok := computeLatency(m.podScheduled, "PodScheduledLatency"); ok {
			m.PodScheduledLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("Pod"+string(corev1.PodScheduled), m.PodScheduledLatency))
		}
		if latency, ok := computeLatency(m.podInitialized, "PodInitializedLatency"); ok {
			m.PodInitializedLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("Pod"+string(corev1.PodInitialized), m.PodInitializedLatency))
		}
		if latency, ok := computeLatency(m.podContainersReady, "PodContainersReadyLatency"); ok {
			m.PodContainersReadyLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("Pod"+string(corev1.ContainersReady), m.PodContainersReadyLatency))
		}
		if latency, ok := computeLatency(m.podReady, "PodReadyLatency"); ok {
			m.PodReadyLatency = latency
			vmi.NormLatencies = append(vmi.NormLatencies, makeDoc("Pod"+string(corev1.PodReady), m.PodReadyLatency))
		}

		totalVMIs++
		erroredVMIs += errorFlag
		return true
	})
	if totalVMIs == 0 {
		return 0.0
	}
	return float64(erroredVMIs) / float64(totalVMIs) * 100.0
}

func (vmi *vmiLatency) getLatency(normLatency any) map[string]float64 {
	doc := normLatency.(metrics.LatencyDocument)
	condition := doc.Labels["condition"]
	return map[string]float64{condition: doc.Value}
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

func (vmi *vmiLatency) IsCompatible() bool {
	_, exists := supportedVMILatencyJobTypes[vmi.JobConfig.JobType]
	return exists
}

// virtualMachineTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: conditions
func virtualMachineTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := createMinimalUnstructured(u, defaultMetadataTransformOpts())

		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}

// virtualMachineInstanceTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels, ownerReferences
// - status: phase
func virtualMachineInstanceTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := createMinimalUnstructured(u, metadataTransformOptions{
			includeNamespace:       true,
			includeLabels:          true,
			includeOwnerReferences: true,
		})

		if phase, found, _ := unstructured.NestedString(u.Object, "status", "phase"); found {
			_ = unstructured.SetNestedField(minimal.Object, phase, "status", "phase")
		}

		return minimal, nil
	}
}
