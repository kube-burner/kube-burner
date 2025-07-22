// Copyright 2025 The Kube-burner Authors.
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
	"strings"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	kvv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
)

const (
	vmimLatencyMeasurement          = "vmimLatencyMeasurement"
	vmimLatencyQuantilesMeasurement = "vmimLatencyQuantilesMeasurement"
)

var (
	supportedVMIMConditions = map[string]struct{}{
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationPending):         {},
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationScheduling):      {},
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationScheduled):       {},
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationPreparingTarget): {},
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationTargetReady):     {},
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationRunning):         {},
		"VirtualMachineInstanceMigration" + string(kvv1.MigrationSucceeded):       {},
	}
)

// vmimMetric holds VirtualMachineInstanceMigration metrics
type vmimMetric struct {
	// Timestamp field is very important for elasticsearch indexing and represents the creation time
	Timestamp time.Time `json:"timestamp"`

	pendingTime            time.Time
	PendingLatency         int `json:"pendingLatency"`
	schedulingTime         time.Time
	SchedulingLatency      int `json:"schedulingLatency"`
	scheduledTime          time.Time
	ScheduledLatency       int `json:"scheduledLatency"`
	preparingTargetTime    time.Time
	PreparingTargetLatency int `json:"preparingTargetLatency"`
	targetReadyTime        time.Time
	TargetReadyLatency     int `json:"targetReadyLatency"`
	runningTime            time.Time
	RunningLatency         int `json:"runningLatency"`
	succeededTime          time.Time
	SucceededLatency       int `json:"succeededLatency"`

	MetricName   string `json:"metricName"`
	UUID         string `json:"uuid"`
	Namespace    string `json:"namespace"`
	Name         string `json:"vmimName"`
	VMIName      string `json:"vmiName"`
	JobName      string `json:"jobName,omitempty"`
	JobIteration int    `json:"jobIteration"`
	Replica      int    `json:"replica"`
	Metadata     any    `json:"metadata,omitempty"`
}

type vmimLatency struct {
	BaseMeasurement
}

type vmimLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newVmimLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedVMIMConditions); err != nil {
		return nil, err
	}
	return vmimLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (vmimf vmimLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &vmimLatency{
		BaseMeasurement: vmimf.NewBaseLatency(jobConfig, clientSet, restConfig, vmimLatencyMeasurement, vmimLatencyQuantilesMeasurement, embedCfg),
	}
}

func (vmiml *vmimLatency) handleCreateVMIM(obj any) {
	migration, err := util.ConvertAnyToTyped[kvv1.VirtualMachineInstanceMigration](obj)
	if err != nil {
		log.Errorf("failed to convert to VirtualMachineInstanceMigration: %v", err)
		return
	}
	migrationLabels := migration.GetLabels()
	vmiml.metrics.LoadOrStore(string(migration.GetUID()), vmimMetric{
		Namespace:    migration.GetNamespace(),
		MetricName:   vmimLatencyMeasurement,
		UUID:         vmiml.Uuid,
		Name:         migration.GetName(),
		VMIName:      migration.Spec.VMIName,
		JobName:      vmiml.JobConfig.Name,
		JobIteration: getIntFromLabels(migrationLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(migrationLabels, config.KubeBurnerLabelReplica),
		Timestamp:    migration.GetCreationTimestamp().UTC(),
	})
}

func (vmiml *vmimLatency) handleUpdateVMIM(obj any) {
	migration, err := util.ConvertAnyToTyped[kvv1.VirtualMachineInstanceMigration](obj)
	if err != nil {
		log.Errorf("failed to convert to VirtualMachineInstanceMigration: %v", err)
		return
	}
	if vmimM, ok := vmiml.metrics.Load(string(migration.GetUID())); ok {
		vmimMetric := vmimM.(vmimMetric)

		for _, timestamp := range migration.Status.PhaseTransitionTimestamps {
			switch timestamp.Phase {
			case kvv1.MigrationPending:
				if vmimMetric.pendingTime.IsZero() {
					vmimMetric.pendingTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			case kvv1.MigrationScheduling:
				if vmimMetric.schedulingTime.IsZero() {
					vmimMetric.schedulingTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			case kvv1.MigrationScheduled:
				if vmimMetric.scheduledTime.IsZero() {
					vmimMetric.scheduledTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			case kvv1.MigrationPreparingTarget:
				if vmimMetric.preparingTargetTime.IsZero() {
					vmimMetric.preparingTargetTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			case kvv1.MigrationTargetReady:
				if vmimMetric.targetReadyTime.IsZero() {
					vmimMetric.targetReadyTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			case kvv1.MigrationRunning:
				if vmimMetric.runningTime.IsZero() {
					vmimMetric.runningTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			case kvv1.MigrationSucceeded:
				if vmimMetric.succeededTime.IsZero() {
					vmimMetric.succeededTime = timestamp.PhaseTransitionTimestamp.UTC()
				}
			}
		}

		vmiml.metrics.Store(string(migration.GetUID()), vmimMetric)
	}
}

// Start starts vmimLatency measurement
func (vmiml *vmimLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	gvr, err := util.ResourceToGVR(vmiml.RestConfig, "VirtualMachineInstanceMigration", "kubevirt.io/v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "VirtualMachineInstanceMigration", err)
	}
	vmiml.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(vmiml.RestConfig),
				name:          "vmimWatcher",
				resource:      gvr,
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", vmiml.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vmiml.handleCreateVMIM,
					UpdateFunc: func(oldObj, newObj any) {
						vmiml.handleUpdateVMIM(newObj)
					},
				},
			},
		},
	)
	return nil
}

func (vmiml *vmimLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var vmims []kvv1.VirtualMachineInstanceMigration
	labelSelector := labels.SelectorFromSet(vmiml.JobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	kubeVirtClient, err := kubecli.GetKubevirtClientFromRESTConfig(vmiml.RestConfig)
	if err != nil {
		log.Fatalf("Failed to get kubevirt client - %v", err)
	}
	namespaces := strings.Split(vmiml.JobConfig.Namespace, ",")
	for _, namespace := range namespaces {
		vmimList, err := kubeVirtClient.VirtualMachineInstanceMigration(namespace).List(context.TODO(), options)
		if err != nil {
			log.Errorf("error listing dataVolumes in namespace %s: %v", namespace, err)
		}
		vmims = append(vmims, vmimList.Items...)
	}

	vmiml.metrics = sync.Map{}
	for _, vmim := range vmims {
		var pending, scheduling, scheduled, preparingTarget, targetReady, running, succeeded time.Time
		for _, timestamp := range vmim.Status.PhaseTransitionTimestamps {
			switch timestamp.Phase {
			case kvv1.MigrationPending:
				pending = timestamp.PhaseTransitionTimestamp.UTC()
			case kvv1.MigrationScheduling:
				scheduling = timestamp.PhaseTransitionTimestamp.UTC()
			case kvv1.MigrationScheduled:
				scheduled = timestamp.PhaseTransitionTimestamp.UTC()
			case kvv1.MigrationPreparingTarget:
				preparingTarget = timestamp.PhaseTransitionTimestamp.UTC()
			case kvv1.MigrationTargetReady:
				targetReady = timestamp.PhaseTransitionTimestamp.UTC()
			case kvv1.MigrationRunning:
				running = timestamp.PhaseTransitionTimestamp.UTC()
			case kvv1.MigrationSucceeded:
				succeeded = timestamp.PhaseTransitionTimestamp.UTC()
			}
		}
		vmiml.metrics.Store(string(vmim.GetUID()), vmimMetric{
			MetricName:          vmimLatencyMeasurement,
			UUID:                vmiml.Uuid,
			Namespace:           vmim.GetNamespace(),
			Name:                vmim.GetName(),
			VMIName:             vmim.Spec.VMIName,
			JobName:             vmiml.JobConfig.Name,
			JobIteration:        getIntFromLabels(vmim.GetLabels(), config.KubeBurnerLabelJobIteration),
			Replica:             getIntFromLabels(vmim.GetLabels(), config.KubeBurnerLabelReplica),
			Timestamp:           vmim.GetCreationTimestamp().UTC(),
			pendingTime:         pending,
			schedulingTime:      scheduling,
			scheduledTime:       scheduled,
			preparingTargetTime: preparingTarget,
			targetReadyTime:     targetReady,
			runningTime:         running,
			succeededTime:       succeeded,
		})
	}
}

// Stop stops vmimLatency measurement
func (vmiml *vmimLatency) Stop() error {
	return vmiml.StopMeasurement(vmiml.normalizeMetrics, vmiml.getLatency)
}

func (vmiml *vmimLatency) normalizeMetrics() float64 {
	count := 0
	errored := 0

	vmiml.metrics.Range(func(key, value any) bool {
		m := value.(vmimMetric)

		if m.succeededTime.IsZero() {
			log.Tracef("VirtualMachineInstanceMigration %v latency ignored as it did not reach Succeeded state", m.Name)
			return true
		}

		errorFlag := 0
		// Calculate latencies from the timestamp (creation time)
		m.PendingLatency = int(m.pendingTime.Sub(m.Timestamp).Milliseconds())
		if m.PendingLatency < 0 {
			log.Tracef("PendingLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.PendingLatency = 0
		}

		m.SchedulingLatency = int(m.schedulingTime.Sub(m.Timestamp).Milliseconds())
		if m.SchedulingLatency < 0 {
			log.Tracef("SchedulingLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.SchedulingLatency = 0
		}

		m.ScheduledLatency = int(m.scheduledTime.Sub(m.Timestamp).Milliseconds())
		if m.ScheduledLatency < 0 {
			log.Tracef("ScheduledLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.ScheduledLatency = 0
		}

		m.PreparingTargetLatency = int(m.preparingTargetTime.Sub(m.Timestamp).Milliseconds())
		if m.PreparingTargetLatency < 0 {
			log.Tracef("PreparingTargetLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.PreparingTargetLatency = 0
		}

		m.TargetReadyLatency = int(m.targetReadyTime.Sub(m.Timestamp).Milliseconds())
		if m.TargetReadyLatency < 0 {
			log.Tracef("TargetReadyLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.TargetReadyLatency = 0
		}

		m.RunningLatency = int(m.runningTime.Sub(m.Timestamp).Milliseconds())
		if m.RunningLatency < 0 {
			log.Tracef("RunningLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.RunningLatency = 0
		}

		m.SucceededLatency = int(m.succeededTime.Sub(m.Timestamp).Milliseconds())
		if m.SucceededLatency < 0 {
			log.Tracef("SucceededLatency for VirtualMachineInstanceMigration %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.SucceededLatency = 0
		}

		count++
		errored += errorFlag
		vmiml.normLatencies = append(vmiml.normLatencies, m)
		return true
	})

	if count == 0 {
		return 0.0
	}
	return float64(errored) / float64(count) * 100.0
}

func (vmiml *vmimLatency) getLatency(normLatency any) map[string]float64 {
	vmimMetric := normLatency.(vmimMetric)
	return map[string]float64{
		string(kvv1.MigrationPending):         float64(vmimMetric.PendingLatency),
		string(kvv1.MigrationScheduling):      float64(vmimMetric.SchedulingLatency),
		string(kvv1.MigrationScheduled):       float64(vmimMetric.ScheduledLatency),
		string(kvv1.MigrationPreparingTarget): float64(vmimMetric.PreparingTargetLatency),
		string(kvv1.MigrationTargetReady):     float64(vmimMetric.TargetReadyLatency),
		string(kvv1.MigrationRunning):         float64(vmimMetric.RunningLatency),
		string(kvv1.MigrationSucceeded):       float64(vmimMetric.SucceededLatency),
	}
}
