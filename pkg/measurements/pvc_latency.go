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

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

type pvcMetric struct {
	Timestamp      time.Time `json:"timestamp"`
	pending        int64
	PendingLatency int `json:"pendingLatency"`
	bound          int64
	BindingLatency int `json:"bindingLatency"`
	lost           int64
	LostLatency    int         `json:"lostLatency"`
	UUID           string      `json:"uuid"`
	Name           string      `json:"pvcName"`
	JobName        string      `json:"jobName,omitempty"`
	Namespace      string      `json:"namespace"`
	MetricName     string      `json:"metricName"`
	Size           string      `json:"size"`
	StorageClass   string      `json:"storageClass"`
	JobIteration   int         `json:"jobIteration"`
	Replica        int         `json:"replica"`
	Metadata       interface{} `json:"metadata,omitempty"`
}

type pvcLatency struct {
	baseMeasurement
	watcher          *metrics.Watcher
	metrics          sync.Map
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

type pvcLatencyMeasurementFactory struct {
	baseMeasurementFactory
}

func newPvcLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]interface{}) (measurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedPvcConditions); err != nil {
		return nil, err
	}
	return pvcLatencyMeasurementFactory{
		baseMeasurementFactory: newBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (plmf pvcLatencyMeasurementFactory) newMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) measurement {
	return &pvcLatency{
		baseMeasurement: plmf.newBaseLatency(jobConfig, clientSet, restConfig),
	}
}

// creates pvc metric
func (p *pvcLatency) handleCreatePVC(obj interface{}) {
	pvc := obj.(*corev1.PersistentVolumeClaim)
	log.Tracef("handleCreatePVC: %s", pvc.Name)
	pvcLabels := pvc.GetLabels()
	p.metrics.LoadOrStore(string(pvc.UID), pvcMetric{
		Timestamp:    time.Now().UTC(),
		Namespace:    pvc.Namespace,
		Name:         pvc.Name,
		StorageClass: getStorageClassName(*pvc),
		Size:         pvc.Spec.Resources.Requests.Storage().String(),
		MetricName:   pvcLatencyMeasurement,
		UUID:         p.uuid,
		JobName:      p.jobConfig.Name,
		Metadata:     p.metadata,
		JobIteration: getIntFromLabels(pvcLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(pvcLabels, config.KubeBurnerLabelReplica),
	})
}

// handles pvc update
func (p *pvcLatency) handleUpdatePVC(obj interface{}) {
	pvc := obj.(*corev1.PersistentVolumeClaim)
	log.Tracef("handleUpdatePVC: %s", pvc.Name)
	if value, exists := p.metrics.Load(string(pvc.UID)); exists {
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
			p.metrics.Store(string(pvc.UID), pm)
		} else {
			log.Tracef("Skipping update for phase [%s] as PVC is already bound or lost", pvc.Status.Phase)
		}
	}
}

// start pvcLatency measurement
func (p *pvcLatency) start(measurementWg *sync.WaitGroup) error {
	if p.jobConfig.JobType == config.ReadJob || p.jobConfig.JobType == config.PatchJob || p.jobConfig.JobType == config.DeletionJob {
		log.Fatalf("Unsupported jobType:%s for pvcLatency metric", p.jobConfig.JobType)
	}
	p.latencyQuantiles, p.normLatencies = nil, nil
	defer measurementWg.Done()
	p.metrics = sync.Map{}
	log.Infof("Creating PVC latency watcher for %s", p.jobConfig.Name)
	p.watcher = metrics.NewWatcher(
		p.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"pvcWatcher",
		"persistentvolumeclaims",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", p.runid)
		},
		nil,
	)
	p.watcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: p.handleCreatePVC,
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.handleUpdatePVC(newObj)
		},
	})
	if err := p.watcher.StartAndCacheSync(); err != nil {
		log.Errorf("PVC Latency measurement error: %s", err)
	}
	return nil
}

// collects PVC measurements triggered in the past
func (p *pvcLatency) collect(measurementWg *sync.WaitGroup) {
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
func (p *pvcLatency) stop() error {
	var err error
	defer func() {
		if p.watcher != nil {
			p.watcher.StopWatcher()
		}
	}()
	errorRate := p.normalizeMetrics()
	if errorRate > 10.00 {
		log.Error("Latency errors beyond 10%. Hence invalidating the results")
		return fmt.Errorf("Something is wrong with system under test. PVC latencies error rate was: %.2f", errorRate)
	}
	p.calcQuantiles()
	if len(p.config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(p.config.LatencyThresholds, p.latencyQuantiles)
	}
	for _, q := range p.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %v 99th: %v max: %v avg: %v", p.jobConfig.Name, pq.QuantileName, pq.P99, pq.Max, pq.Avg)
	}
	if errorRate > 0 {
		log.Infof("PVC latencies error rate was: %.2f", errorRate)
	}
	return err
}

// index sends metrics to the configured indexer
func (p *pvcLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]interface{}{
		pvcLatencyMeasurement:          p.normLatencies,
		pvcLatencyQuantilesMeasurement: p.latencyQuantiles,
	}
	IndexLatencyMeasurement(p.config, jobName, metricMap, indexerList)
}

// getter function to get metrics
func (p *pvcLatency) getMetrics() *sync.Map {
	return &p.metrics
}

// normalizes pvc latency metrics
func (p *pvcLatency) normalizeMetrics() float64 {
	totalPVCs := 0
	erroredPVCs := 0

	p.metrics.Range(func(key, value interface{}) bool {
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
		p.normLatencies = append(p.normLatencies, m)
		return true
	})
	if totalPVCs == 0 {
		return 0.0
	}
	return float64(erroredPVCs) / float64(totalPVCs) * 100.0
}

// calculates latency quantiles
func (p *pvcLatency) calcQuantiles() {
	getLatency := func(normLatency interface{}) map[string]float64 {
		pvcMetric := normLatency.(pvcMetric)
		return map[string]float64{
			string(corev1.ClaimPending): float64(pvcMetric.PendingLatency),
			string(corev1.ClaimBound):   float64(pvcMetric.BindingLatency),
			string(corev1.ClaimLost):    float64(pvcMetric.LostLatency),
		}
	}
	p.latencyQuantiles = calculateQuantiles(p.uuid, p.jobConfig.Name, p.metadata, p.normLatencies, getLatency, pvcLatencyQuantilesMeasurement)
}
