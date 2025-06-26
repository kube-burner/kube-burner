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

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
	"kubevirt.io/client-go/kubecli"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
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
	BaseMeasurement
}

type volumeSnapshotLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newvolumeSnapshotLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedVolumeSnapshotConditions); err != nil {
		return nil, err
	}
	return volumeSnapshotLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (vslmf volumeSnapshotLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &volumeSnapshotLatency{
		BaseMeasurement: vslmf.NewBaseLatency(jobConfig, clientSet, restConfig, volumeSnapshotLatencyMeasurement, volumeSnapshotLatencyQuantilesMeasurement, embedCfg),
	}
}

func (vsl *volumeSnapshotLatency) handleCreateVolumeSnapshot(obj any) {
	volumeSnapshot := obj.(*volumesnapshotv1.VolumeSnapshot)
	vsLabels := volumeSnapshot.GetLabels()
	vsl.metrics.LoadOrStore(string(volumeSnapshot.UID), volumeSnapshotMetric{
		Timestamp:    volumeSnapshot.CreationTimestamp.UTC(),
		Namespace:    volumeSnapshot.Namespace,
		Name:         volumeSnapshot.Name,
		MetricName:   volumeSnapshotLatencyMeasurement,
		UUID:         vsl.Uuid,
		JobName:      vsl.JobConfig.Name,
		Metadata:     vsl.Metadata,
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

func (vsl *volumeSnapshotLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	vsl.startMeasurement(
		[]MeasurementWatcher{
			{
				restClient:    getGroupVersionClient(vsl.RestConfig, volumesnapshotv1.SchemeGroupVersion, &volumesnapshotv1.VolumeSnapshotList{}, &volumesnapshotv1.VolumeSnapshot{}),
				name:          "vsWatcher",
				resource:      "volumesnapshots",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", vsl.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: vsl.handleCreateVolumeSnapshot,
					UpdateFunc: func(oldObj, newObj any) {
						vsl.handleUpdateVolumeSnapshot(newObj)
					},
				},
			},
		},
	)
	return nil
}

func (vsl *volumeSnapshotLatency) Stop() error {
	return vsl.StopMeasurement(vsl.normalizeMetrics, vsl.getLatency)
}

func (vsl *volumeSnapshotLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var volumeSnapshots []volumesnapshotv1.VolumeSnapshot
	labelSelector := labels.SelectorFromSet(vsl.JobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	kubeVirtClient, err := kubecli.GetKubevirtClientFromRESTConfig(vsl.RestConfig)
	if err != nil {
		log.Fatalf("Failed to get kubevirt client - %v", err)
	}
	namespaces := strings.Split(vsl.JobConfig.Namespace, ",")
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
			Timestamp:  volumeSnapshot.CreationTimestamp.UTC(),
			Namespace:  volumeSnapshot.Namespace,
			Name:       volumeSnapshot.Name,
			MetricName: volumeSnapshotLatencyMeasurement,
			UUID:       vsl.Uuid,
			vsReady:    volumeSnapshot.Status.CreationTime.Time,
			JobName:    vsl.JobConfig.Name,
		})
	}
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

func (vsl *volumeSnapshotLatency) getLatency(normLatency any) map[string]float64 {
	volumeSnapshotMetric := normLatency.(volumeSnapshotMetric)
	return map[string]float64{
		"Ready": float64(volumeSnapshotMetric.VSReadyLatency),
	}
}
