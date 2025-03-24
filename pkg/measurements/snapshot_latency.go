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

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
	"kubevirt.io/client-go/kubecli"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
)

const (
	volumeSnapshotLatencyMeasurement          = "volumeSnapshotLatencyMeasurement"
	volumeSnapshotLatencyQuantilesMeasurement = "volumeSnapshotLatencyQuantilesMeasurement"
)

var (
	supportedVolumeSnapshotConditions = map[string]struct{}{
		"Ready": {},
	}
)

// volumeSnapshotMetric holds data about VolumeSnapshot creation process
type volumeSnapshotMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	Timestamp time.Time `json:"timestamp"`

	vsReady        time.Time
	VSReadyLatency int `json:"vsReadyLatency"`

	MetricName   string `json:"metricName"`
	UUID         string `json:"uuid"`
	Namespace    string `json:"namespace"`
	Name         string `json:"vsName"`
	JobName      string `json:"jobName,omitempty"`
	JobIteration int    `json:"jobIteration"`
	Replica      int    `json:"replica"`
	Metadata     any    `json:"metadata,omitempty"`
}

type volumeSnapshotLatency struct {
	baseLatencyMeasurement

	watcher          *metrics.Watcher
	metrics          sync.Map
	latencyQuantiles []any
	normLatencies    []any
}

type volumeSnapshotLatencyMeasurementFactory struct {
	baseLatencyMeasurementFactory
}

func newvolumeSnapshotLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (measurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedVolumeSnapshotConditions); err != nil {
		return nil, err
	}
	return volumeSnapshotLatencyMeasurementFactory{
		baseLatencyMeasurementFactory: newBaseLatencyMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (vslmf volumeSnapshotLatencyMeasurementFactory) newMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) measurement {
	return &volumeSnapshotLatency{
		baseLatencyMeasurement: vslmf.newBaseLatency(jobConfig, clientSet, restConfig),
	}
}

func (vsl *volumeSnapshotLatency) handleCreateVolumeSnapshot(obj any) {
	volumeSnapshot := obj.(*volumesnapshotv1.VolumeSnapshot)
	vsLabels := volumeSnapshot.GetLabels()
	vsl.metrics.LoadOrStore(string(volumeSnapshot.UID), volumeSnapshotMetric{
		Timestamp:    volumeSnapshot.CreationTimestamp.Time.UTC(),
		Namespace:    volumeSnapshot.Namespace,
		Name:         volumeSnapshot.Name,
		MetricName:   volumeSnapshotLatencyMeasurement,
		UUID:         vsl.uuid,
		JobName:      vsl.jobConfig.Name,
		Metadata:     vsl.metadata,
		JobIteration: getIntFromLabels(vsLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(vsLabels, config.KubeBurnerLabelReplica),
	})
}

func (vsl *volumeSnapshotLatency) handleUpdateVolumeSnapshot(obj any) {
	volumeSnapshot := obj.(*volumesnapshotv1.VolumeSnapshot)
	if value, exists := vsl.metrics.Load(string(volumeSnapshot.UID)); exists {
		vsm := value.(volumeSnapshotMetric)
		if vsm.vsReady.IsZero() {
			if volumeSnapshot.Status != nil && ptr.Deref(volumeSnapshot.Status.ReadyToUse, false) {
				log.Debugf("Updated ready time for volumeSnapshot [%s]", volumeSnapshot.Name)
				vsm.vsReady = time.Now().UTC()
			}
		}
		vsl.metrics.Store(string(volumeSnapshot.UID), vsm)
	}
}

func (vsl *volumeSnapshotLatency) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Reset latency slices, required in multi-job benchmarks
	vsl.latencyQuantiles, vsl.normLatencies = nil, nil
	vsl.metrics = sync.Map{}
	log.Infof("Creating Data Volume latency watcher for %s", vsl.jobConfig.Name)
	vsl.watcher = metrics.NewWatcher(
		getGroupVersionClient(vsl.restConfig, volumesnapshotv1.SchemeGroupVersion, &volumesnapshotv1.VolumeSnapshotList{}, &volumesnapshotv1.VolumeSnapshot{}),
		"vsWatcher",
		"volumesnapshots",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", vsl.runid)
		},
		nil,
	)
	vsl.watcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: vsl.handleCreateVolumeSnapshot,
		UpdateFunc: func(oldObj, newObj any) {
			vsl.handleUpdateVolumeSnapshot(newObj)
		},
	})
	if err := vsl.watcher.StartAndCacheSync(); err != nil {
		log.Errorf("VolumeSnapshot Latency measurement error: %s", err)
	}

	return nil
}

func (vsl *volumeSnapshotLatency) stop() error {
	var err error
	defer func() {
		if vsl.watcher != nil {
			vsl.watcher.StopWatcher()
		}
	}()
	errorRate := vsl.normalizeMetrics()
	if errorRate > 10.00 {
		log.Error("Latency errors beyond 10%. Hence invalidating the results")
		return fmt.Errorf("something is wrong with system under test. VolumeSnapshot latencies error rate was: %.2f", errorRate)
	}
	vsl.calcQuantiles()
	if len(vsl.config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(vsl.config.LatencyThresholds, vsl.latencyQuantiles)
	}
	for _, q := range vsl.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %v 99th: %v max: %v avg: %v", vsl.jobConfig.Name, pq.QuantileName, pq.P99, pq.Max, pq.Avg)
	}
	if errorRate > 0 {
		log.Infof("DataVolume latencies error rate was: %.2f", errorRate)
	}
	return err
}

func (vsl *volumeSnapshotLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var volumeSnapshots []volumesnapshotv1.VolumeSnapshot
	labelSelector := labels.SelectorFromSet(vsl.jobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	kubeVirtClient, err := kubecli.GetKubevirtClientFromRESTConfig(vsl.restConfig)
	if err != nil {
		log.Fatalf("Failed to get kubevirt client - %v", err)
	}
	namespaces := strings.Split(vsl.jobConfig.Namespace, ",")
	for _, namespace := range namespaces {
		vsList, err := kubeVirtClient.KubernetesSnapshotClient().SnapshotV1().VolumeSnapshots(namespace).List(context.TODO(), options)
		if err != nil {
			log.Errorf("error listing volumeSnapshots in namespace %s: %v", namespace, err)
		}
		volumeSnapshots = append(volumeSnapshots, vsList.Items...)
	}
	vsl.metrics = sync.Map{}
	for _, volumeSnapshot := range volumeSnapshots {
		vsl.metrics.Store(string(volumeSnapshot.UID), volumeSnapshotMetric{
			Timestamp:  volumeSnapshot.CreationTimestamp.Time.UTC(),
			Namespace:  volumeSnapshot.Namespace,
			Name:       volumeSnapshot.Name,
			MetricName: volumeSnapshotLatencyMeasurement,
			UUID:       vsl.uuid,
			vsReady:    volumeSnapshot.Status.CreationTime.Time,
			JobName:    vsl.jobConfig.Name,
		})
	}
}

func (vsl *volumeSnapshotLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]any{
		volumeSnapshotLatencyMeasurement:          vsl.normLatencies,
		volumeSnapshotLatencyQuantilesMeasurement: vsl.latencyQuantiles,
	}
	IndexLatencyMeasurement(vsl.config, jobName, metricMap, indexerList)
}

func (vsl *volumeSnapshotLatency) getMetrics() *sync.Map {
	return &vsl.metrics
}

func (vsl *volumeSnapshotLatency) normalizeMetrics() float64 {
	volumeSnapshotCount := 0
	erroredVolumeSnapshots := 0

	vsl.metrics.Range(func(key, value any) bool {
		m := value.(volumeSnapshotMetric)
		// Skip VolumeSnapshot if it did not reach the Ready state (this timestamp isn't set)
		if m.vsReady.IsZero() {
			log.Warningf("VolumeSnapshot %v latency ignored as it did not reach Ready state", m.Name)
			return true
		}
		// latencyTime should be always larger than zero, however, in some cases, it might be a
		// negative value due to the precision of timestamp can only get to the level of second
		// and also the creation timestamp we capture using time.Now().UTC() might even have a
		// delay over 1s in some cases. The microsecond and nanosecond have been discarded purposely
		// in kubelet, this is because apiserver does not support RFC339NANO. The newly introduced
		// v2 latencies are currently under AB testing which blindly trust kubernetes as source of
		// truth and will prevent us from those over 1s delays as well as <0 cases.
		errorFlag := 0
		m.VSReadyLatency = int(m.vsReady.Sub(m.Timestamp).Milliseconds())
		if m.VSReadyLatency < 0 {
			log.Tracef("VSReadyLatency for VolumeSnapshot %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.VSReadyLatency = 0
		}
		volumeSnapshotCount++
		erroredVolumeSnapshots += errorFlag
		vsl.normLatencies = append(vsl.normLatencies, m)
		return true
	})
	if volumeSnapshotCount == 0 {
		return 0.0
	}
	return float64(erroredVolumeSnapshots) / float64(volumeSnapshotCount) * 100.0
}

func (vsl *volumeSnapshotLatency) calcQuantiles() {
	getLatency := func(normLatency any) map[string]float64 {
		volumeSnapshotMetric := normLatency.(volumeSnapshotMetric)
		return map[string]float64{
			"Ready": float64(volumeSnapshotMetric.VSReadyLatency),
		}
	}
	vsl.latencyQuantiles = calculateQuantiles(vsl.uuid, vsl.jobConfig.Name, vsl.metadata, vsl.normLatencies, getLatency, volumeSnapshotLatencyQuantilesMeasurement)
}
