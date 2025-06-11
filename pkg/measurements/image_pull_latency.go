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
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	imagePullLatencyMeasurement          = "imagePullLatencyMeasurement"
	imagePullLatencyQuantilesMeasurement = "imagePullLatencyQuantilesMeasurement"
)

type imagePullMetric struct {
	Timestamp     time.Time `json:"timestamp"`
	PodName       string    `json:"podName"`
	ContainerName string    `json:"containerName"`
	Image         string    `json:"image"`
	PullStartTime time.Time `json:"pullStartTime"`
	PullEndTime   time.Time `json:"pullEndTime"`
	PullLatency   int       `json:"pullLatency"`
	MetricName    string    `json:"metricName"`
	UUID          string    `json:"uuid"`
	JobName       string    `json:"jobName,omitempty"`
	JobIteration  int       `json:"jobIteration"`
	Replica       int       `json:"replica"`
	Namespace     string    `json:"namespace"`
	NodeName      string    `json:"nodeName"`
	Metadata      any       `json:"metadata,omitempty"`
}

type imagePullLatency struct {
	BaseMeasurement
	Ctx context.Context
}

type imagePullLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newImagePullLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	return imagePullLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (iplmf imagePullLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) Measurement {
	return &imagePullLatency{
		BaseMeasurement: iplmf.NewBaseLatency(jobConfig, clientSet, restConfig, imagePullLatencyMeasurement, imagePullLatencyQuantilesMeasurement),
		Ctx:             context.Background(),
	}
}

func (ipl *imagePullLatency) handleCreatePod(obj any) {
	pod := obj.(*corev1.Pod)
	podLabels := pod.GetLabels()
	ipl.metrics.LoadOrStore(string(pod.UID), imagePullMetric{
		Timestamp:    pod.CreationTimestamp.Time.UTC(),
		PodName:      pod.Name,
		Namespace:    pod.Namespace,
		MetricName:   imagePullLatencyMeasurement,
		UUID:         ipl.Uuid,
		JobName:      ipl.JobConfig.Name,
		Metadata:     ipl.Metadata,
		JobIteration: getIntFromLabels(podLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(podLabels, config.KubeBurnerLabelReplica),
		NodeName:     pod.Spec.NodeName,
	})
}

func (ipl *imagePullLatency) handleUpdatePod(obj any) {
	pod := obj.(*corev1.Pod)
	if value, exists := ipl.metrics.Load(string(pod.UID)); exists {
		ipm := value.(imagePullMetric)
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "Pulling" {
				ipm.ContainerName = containerStatus.Name
				ipm.Image = containerStatus.Image
				ipm.PullStartTime = time.Now().UTC()
			} else if containerStatus.State.Running != nil && ipm.PullEndTime.IsZero() {
				ipm.PullEndTime = time.Now().UTC()
				ipm.PullLatency = int(ipm.PullEndTime.Sub(ipm.PullStartTime).Milliseconds())
			}
		}
		ipl.metrics.Store(string(pod.UID), ipm)
	}
}

func (ipl *imagePullLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	ipl.startMeasurement(
		[]MeasurementWatcher{
			{
				restClient:    ipl.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
				name:          "podWatcher",
				resource:      "pods",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", ipl.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: ipl.handleCreatePod,
					UpdateFunc: func(oldObj, newObj any) {
						ipl.handleUpdatePod(newObj)
					},
				},
			},
		},
	)
	return nil
}

func (ipl *imagePullLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	// Collect metrics for pods that were created before the measurement started
	pods, err := ipl.ClientSet.CoreV1().Pods("").List(ipl.Ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kube-burner-runid=%v", ipl.Runid),
	})
	if err != nil {
		logrus.Errorf("Error collecting image pull metrics: %v", err)
		return
	}
	for _, pod := range pods.Items {
		ipl.handleCreatePod(&pod)
		ipl.handleUpdatePod(&pod)
	}
}

func (ipl *imagePullLatency) Stop() error {
	return ipl.StopMeasurement(ipl.normalizeMetrics, ipl.getLatency)
}

func (ipl *imagePullLatency) normalizeMetrics() float64 {
	var totalLatency float64
	var count int
	ipl.metrics.Range(func(_, value any) bool {
		ipm := value.(imagePullMetric)
		if ipm.PullLatency > 0 {
			totalLatency += float64(ipm.PullLatency)
			count++
		}
		return true
	})
	if count == 0 {
		return 0
	}
	return totalLatency / float64(count)
}

func (ipl *imagePullLatency) getLatency(normLatency any) map[string]float64 {
	ipm := normLatency.(imagePullMetric)
	return map[string]float64{
		"pullLatency": float64(ipm.PullLatency),
	}
}
