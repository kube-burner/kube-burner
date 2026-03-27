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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
		"Resized":                   {},
	}
	supportedPvcLatencyJobTypes = map[config.JobType]struct{}{
		config.CreationJob: {},
		config.PatchJob:    {},
		config.DeletionJob: {},
	}
)

type pvcLatencyLabels struct {
	JobIteration int    `json:"jobIteration"`
	Replica      int    `json:"replica"`
	Namespace    string `json:"namespace"`
	Name         string `json:"pvcName"`
	Size         string `json:"size"`
	StorageClass string `json:"storageClass"`
}
type pvcMetric struct {
	metrics.LatencyDocument
	pending          int64
	PendingLatency   int `json:"pendingLatency"`
	bound            int64
	BindingLatency   int `json:"bindingLatency"`
	lost             int64
	LostLatency      int `json:"lostLatency"`
	resizeStarted    int64
	ResizeLatency    int              `json:"resizeLatency"`
	ResizedCapacity  string           `json:"resizedCapacity,omitempty"`
	PvcLatencyLabels pvcLatencyLabels `json:"labels"`
}

type pvcLatency struct {
	BaseMeasurement
}

type pvcLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newPvcLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any, labelSelector string) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedPvcConditions); err != nil {
		return nil, err
	}
	return pvcLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata, labelSelector),
	}, nil
}

func (plmf pvcLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &pvcLatency{
		BaseMeasurement: plmf.NewBaseLatency(jobConfig, clientSet, restConfig, pvcLatencyMeasurement, pvcLatencyQuantilesMeasurement, embedCfg),
	}
}

// creates pvc metric
func (p *pvcLatency) handleCreatePVC(obj any) {
	var err error
	// Verify that object type is pvc
	pvc, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		pvc, err = util.ConvertAnyToTyped[corev1.PersistentVolumeClaim](obj)
		if err != nil {
			log.Errorf("failed to convert to PersistentVolumeClaim: %v", err)
			return
		}
	}
	log.Tracef("handleCreatePVC: %s", pvc.Name)
	pvcLabels := pvc.GetLabels()

	p.Metrics.LoadOrStore(string(pvc.UID), pvcMetric{
		LatencyDocument: metrics.LatencyDocument{
			Timestamp:  time.Now().UTC(),
			MetricName: pvcLatencyMeasurement,
			UUID:       p.Uuid,
			JobName:    p.JobConfig.Name,
			Metadata:   p.Metadata,
		},
		PvcLatencyLabels: pvcLatencyLabels{
			Namespace:    pvc.Namespace,
			Name:         pvc.Name,
			Size:         pvc.Spec.Resources.Requests.Storage().String(),
			StorageClass: getStorageClassName(*pvc),
			JobIteration: getIntFromLabels(pvcLabels, config.KubeBurnerLabelJobIteration),
			Replica:      getIntFromLabels(pvcLabels, config.KubeBurnerLabelReplica),
		},
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
	value, exists := p.Metrics.Load(string(pvc.UID))
	if !exists {
		return
	}
	pm := value.(pvcMetric)

	log.Tracef("handleUpdatePVC: PVC: [%s], Version: [%s], Phase: [%s]", pvc.Name, pvc.ResourceVersion, pvc.Status.Phase)
	now := time.Now().UTC().UnixMilli()
	// 1. Compute requested size first - needed to guard phase tracking
	requestedSize := pvc.Spec.Resources.Requests.Storage().String()

	// 2. Phase Tracking (capture timestamps of first entry into each state)
	if requestedSize == pm.PvcLatencyLabels.Size && (pm.bound == 0 || pm.lost == 0) {
		switch pvc.Status.Phase {
		case corev1.ClaimPending:
			if pm.pending == 0 {
				pm.pending = now
				log.Debugf("PVC %s is pending", pvc.Name)
			}
		case corev1.ClaimBound:
			if pm.bound == 0 {
				pm.bound = now
				log.Debugf("PVC %s is bound", pvc.Name)
			}
		case corev1.ClaimLost:
			if pm.lost == 0 {
				pm.lost = now
				log.Debugf("PVC %s is lost", pvc.Name)
			}
		}
	} else {
		log.Tracef("Skipping phase tracking for PVC %s (phase=%s, sizeChanged=%v)", pvc.Name, pvc.Status.Phase, requestedSize != pm.PvcLatencyLabels.Size)
	}

	// Detect resize start by spec change (works for all provisioners)
	if requestedSize != pm.PvcLatencyLabels.Size && pm.resizeStarted == 0 {
		pm.resizeStarted = now
		log.Debugf("PVC %s resize started: %s -> %s", pvc.Name, pm.PvcLatencyLabels.Size, requestedSize)
	}

	// Check if resize completed by comparing capacity
	if pm.resizeStarted > 0 && pm.ResizeLatency == 0 {
		currentCapacity := pvc.Status.Capacity.Storage().String()
		// If capacity matches requested, it's done
		if currentCapacity == requestedSize {
			pm.ResizeLatency = int(now - pm.resizeStarted)
			pm.ResizedCapacity = currentCapacity
			log.Debugf("PVC %s resize completed (capacity update): %s -> %s in %dms",
				pvc.Name, pm.PvcLatencyLabels.Size, currentCapacity, pm.ResizeLatency)
		}
	}

	p.Metrics.Store(string(pvc.UID), pm)
}

// start pvcLatency measurement
func (p *pvcLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	gvr, err := util.ResourceToGVR(p.RestConfig, "PersistentVolumeClaim", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "PersistentVolumeClaim", err)
	}
	pvcs, err := p.ClientSet.CoreV1().PersistentVolumeClaims("").List(
		context.TODO(),
		metav1.ListOptions{},
	)
	if err != nil {
		log.Errorf("Error listing PVCs for preload: %v", err)
	} else {
		for i := range pvcs.Items {
			p.handleCreatePVC(&pvcs.Items[i])
		}
	}
	p.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(p.RestConfig),
				name:          "pvcWatcher",
				resource:      gvr,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: p.handleCreatePVC,
					UpdateFunc: func(oldObj, newObj any) {
						p.handleUpdatePVC(newObj)
					},
				},
				transform: pvcTransformFunc(),
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
		if m.resizeStarted == 0 && m.bound == 0 && m.lost == 0 {
			log.Tracef("PVC %v latency ignored as it did not reach a stable state", m.PvcLatencyLabels.Name)
			return true
		}
		errorFlag := 0
		m.PendingLatency = int(m.pending - m.Timestamp.UnixMilli())
		if m.PendingLatency < 0 {
			log.Tracef("PendingLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.PvcLatencyLabels.Name)
			if m.pending < 0 {
				errorFlag = 1
			}
			m.PendingLatency = 0
		}
		if m.bound > 0 {
			m.BindingLatency = int(m.bound - m.Timestamp.UnixMilli())
			if m.BindingLatency < 0 {
				log.Tracef("BindingLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.PvcLatencyLabels.Name)
				errorFlag = 1
				m.BindingLatency = 0
			}
		}
		if m.lost > 0 {
			m.LostLatency = int(m.lost - m.Timestamp.UnixMilli())
			if m.LostLatency < 0 {
				log.Tracef("LostLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.PvcLatencyLabels.Name)
				m.LostLatency = 0
			}
		}

		// ResizeLatency is already calculated in handleUpdatePVC, just validate
		if m.ResizeLatency < 0 {
			log.Tracef("ResizeLatency for pvc %v falling under negative case. So explicitly setting it to 0", m.PvcLatencyLabels.Name)
			m.ResizeLatency = 0
		}

		totalPVCs++
		erroredPVCs += errorFlag
		baseLabels := pvcLatencyLabels{
			Namespace:    m.PvcLatencyLabels.Namespace,
			JobIteration: m.PvcLatencyLabels.JobIteration,
			Replica:      m.PvcLatencyLabels.Replica,
			Name:         m.PvcLatencyLabels.Name,
			Size:         m.PvcLatencyLabels.Size,
			StorageClass: m.PvcLatencyLabels.StorageClass,
		}
		makeDoc := func(condition string, valueMs int) metrics.LatencyDocument {
			var lbls map[string]string
			b, _ := json.Marshal(baseLabels)
			_ = json.Unmarshal(b, &lbls)
			lbls["condition"] = condition
			return metrics.LatencyDocument{
				Timestamp:  m.Timestamp,
				MetricName: pvcLatencyMeasurement,
				UUID:       p.Uuid,
				JobName:    p.JobConfig.Name,
				Metadata:   p.Metadata,
				Labels:     lbls,
				Value:      float64(valueMs),
			}
		}
		p.NormLatencies = append(p.NormLatencies,
			makeDoc(string(corev1.ClaimPending), m.PendingLatency),
			makeDoc(string(corev1.ClaimBound), m.BindingLatency),
			makeDoc(string(corev1.ClaimLost), m.LostLatency),
			makeDoc("Resize", m.ResizeLatency),
		)
		return true
	})
	if totalPVCs == 0 {
		return 0.0
	}
	return float64(erroredPVCs) / float64(totalPVCs) * 100.0
}

func (p *pvcLatency) getLatency(normLatency any) map[string]float64 {
	doc := normLatency.(metrics.LatencyDocument)
	condition := doc.Labels["condition"]
	return map[string]float64{condition: doc.Value}
}

func (p *pvcLatency) IsCompatible() bool {
	_, exists := supportedPvcLatencyJobTypes[p.JobConfig.JobType]
	return exists
}

// pvcTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - spec: resources, storageClassName
// - status: phase, conditions, capacity
func pvcTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := createMinimalUnstructured(u, defaultMetadataTransformOpts())

		// Preserve spec fields
		if resources, found, _ := unstructured.NestedMap(u.Object, "spec", "resources"); found {
			_ = unstructured.SetNestedMap(minimal.Object, resources, "spec", "resources")
		}
		if storageClassName, found, _ := unstructured.NestedString(u.Object, "spec", "storageClassName"); found {
			_ = unstructured.SetNestedField(minimal.Object, storageClassName, "spec", "storageClassName")
		}

		// Preserve status fields
		if phase, found, _ := unstructured.NestedString(u.Object, "status", "phase"); found {
			_ = unstructured.SetNestedField(minimal.Object, phase, "status", "phase")
		}
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}
		if capacity, found, _ := unstructured.NestedMap(u.Object, "status", "capacity"); found {
			_ = unstructured.SetNestedMap(minimal.Object, capacity, "status", "capacity")
		}

		return minimal, nil
	}
}
