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
	"fmt"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	pvcLatencyMeasurement          = "pvcLatencyMeasurement"
	pvcLatencyQuantilesMeasurement = "pvcLatencyQuantilesMeasurement"
)

var (
	supportedPvcConditions = map[string]struct{}{
		string(corev1.ClaimPending): {},
		string(corev1.ClaimBound):   {},
		string(corev1.ClaimLost):    {},
	}
	supportedPvcLatencyJobTypes = map[config.JobType]struct{}{
		config.CreationJob: {},
		config.PatchJob:    {},
		config.DeletionJob: {},
	}
)

type pvcMetric struct {
	Timestamp      time.Time `json:"timestamp"`
	pending        int64
	PendingLatency int `json:"pendingLatency"`
	bound          int64
	BindingLatency int `json:"bindingLatency"`
	lost           int64
	LostLatency    int    `json:"lostLatency"`
	UUID           string `json:"uuid"`
	Name           string `json:"pvcName"`
	JobName        string `json:"jobName,omitempty"`
	Namespace      string `json:"namespace"`
	MetricName     string `json:"metricName"`
	Size           string `json:"size"`
	StorageClass   string `json:"storageClass"`
	JobIteration   int    `json:"jobIteration"`
	Replica        int    `json:"replica"`
	Metadata       any    `json:"metadata,omitempty"`
}

type pvcLatency struct {
	BaseMeasurement
}

type pvcLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newPvcLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedPvcConditions); err != nil {
		return nil, err
	}
	return pvcLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (plmf pvcLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &pvcLatency{
		BaseMeasurement: plmf.NewBaseLatency(jobConfig, clientSet, restConfig, pvcLatencyMeasurement, pvcLatencyQuantilesMeasurement, embedCfg),
	}
}

// creates pvc metric
func (p *pvcLatency) handleCreatePVC(obj any) {
	pvc, err := util.ConvertAnyToTyped[corev1.PersistentVolumeClaim](obj)
	if err != nil {
		log.Errorf("failed to convert to PersistentVolumeClaim: %v", err)
		return
	}
	log.Tracef("handleCreatePVC: %s", pvc.Name)
	pvcLabels := pvc.GetLabels()
	p.Metrics.LoadOrStore(string(pvc.UID), pvcMetric{
		Timestamp:    time.Now().UTC(),
		Namespace:    pvc.Namespace,
		Name:         pvc.Name,
		StorageClass: getStorageClassName(*pvc),
		Size:         pvc.Spec.Resources.Requests.Storage().String(),
		MetricName:   pvcLatencyMeasurement,
		UUID:         p.Uuid,
		JobName:      p.JobConfig.Name,
		Metadata:     p.Metadata,
		JobIteration: getIntFromLabels(pvcLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(pvcLabels, config.KubeBurnerLabelReplica),
	})
}

// handles pvc update
func (p *pvcLatency) handleUpdatePVC(obj any) {
	pvc, err := util.ConvertAnyToTyped[corev1.PersistentVolumeClaim](obj)
	if err != nil {
		log.Errorf("failed to convert to PersistentVolumeClaim: %v", err)
		return
	}
	log.Tracef("handleUpdatePVC: %s", pvc.Name)
	if value, exists := p.Metrics.Load(string(pvc.UID)); exists {
		pm := value.(pvcMetric)
		log.Tracef("handleUpdatePVC: PVC: [%s], Version: [%s], Phase: [%s]", pvc.Name, pvc.ResourceVersion, pvc.Status.Phase)
		if pm.bound == 0 || pm.lost == 0 {
			// https://pkg.go.dev/k8s.io/api/core/v1#PersistentVolumeClaimPhase
			if pvc.Status.Phase == corev1.ClaimPending {
				if pm.pending == 0 {
					log.Debugf("PVC %s is pending", pvc.Name)
					pm.pending = time.Now().UTC().UnixMilli()
				}
			}
			if pvc.Status.Phase == corev1.ClaimBound {
				if pm.bound == 0 {
					log.Debugf("PVC %s is bound", pvc.Name)
					pm.bound = time.Now().UTC().UnixMilli()
				}
			}
			if pvc.Status.Phase == corev1.ClaimLost {
				if pm.lost == 0 {
					log.Debugf("PVC %s is lost", pvc.Name)
					pm.lost = time.Now().UTC().UnixMilli()
				}
			}
			p.Metrics.Store(string(pvc.UID), pm)
		} else {
			log.Tracef("Skipping update for phase [%s] as PVC is already bound or lost", pvc.Status.Phase)
		}
	}
}

// start pvcLatency measurement
func (p *pvcLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	if p.JobConfig.JobType == config.ReadJob || p.JobConfig.JobType == config.PatchJob || p.JobConfig.JobType == config.DeletionJob {
		log.Fatalf("Unsupported jobType:%s for pvcLatency metric", p.JobConfig.JobType)
	}
	gvr, err := util.ResourceToGVR(p.RestConfig, "PersistentVolumeClaim", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "PersistentVolumeClaim", err)
	}
	p.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(p.RestConfig),
				name:          "pvcWatcher",
				resource:      gvr,
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", p.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: p.handleCreatePVC,
					UpdateFunc: func(oldObj, newObj any) {
						p.handleUpdatePVC(newObj)
					},
				},
			},
		},
	)
	return nil
}

// collects PVC measurements triggered in the past
func (p *pvcLatency) Collect(measurementWg *sync.WaitGroup) {
	log.Info("Collect method doesn't apply to PVC by design")
	defer measurementWg.Done()
}

// helper to fetch the storage class name (handles nil cases)
func getStorageClassName(pvc corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	return "unknown"
}

// stop pvc latency measurement
func (p *pvcLatency) Stop() error {
	return p.StopMeasurement(p.normalizeMetrics, p.getLatency)
}

// normalizes pvc latency metrics
func (p *pvcLatency) normalizeMetrics() float64 {
	totalPVCs := 0
	erroredPVCs := 0

	p.Metrics.Range(func(key, value any) bool {
		m := value.(pvcMetric)
		// If a pvc does not reach the stable state, we skip that one
		if m.bound == 0 && m.lost == 0 {
			log.Tracef("PVC %v latency ignored as it did not reach a stable state", m.Name)
			return true
		}
		errorFlag := 0
		m.PendingLatency = int(m.pending - m.Timestamp.UnixMilli())
		if m.PendingLatency < 0 {
			log.Tracef("PendingLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.Name)
			if m.pending < 0 {
				errorFlag = 1
			}
			m.PendingLatency = 0
		}

		m.BindingLatency = int(m.bound - m.Timestamp.UnixMilli())
		if m.BindingLatency < 0 {
			log.Tracef("BindingLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.BindingLatency = 0
		}

		m.LostLatency = int(m.lost - m.Timestamp.UnixMilli())
		if m.LostLatency < 0 {
			log.Tracef("LostLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.Name)
			m.LostLatency = 0
		}

		totalPVCs++
		erroredPVCs += errorFlag
		p.NormLatencies = append(p.NormLatencies, m)
		return true
	})
	if totalPVCs == 0 {
		return 0.0
	}
	return float64(erroredPVCs) / float64(totalPVCs) * 100.0
}

func (p *pvcLatency) getLatency(normLatency any) map[string]float64 {
	pvcMetric := normLatency.(pvcMetric)
	return map[string]float64{
		string(corev1.ClaimPending): float64(pvcMetric.PendingLatency),
		string(corev1.ClaimBound):   float64(pvcMetric.BindingLatency),
		string(corev1.ClaimLost):    float64(pvcMetric.LostLatency),
	}
}

func (p *pvcLatency) IsCompatible() bool {
	_, exists := supportedPvcLatencyJobTypes[p.JobConfig.JobType]
	return exists
}
