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

type containerPullMetric struct {
	ContainerName string `json:"containerName"`
	Image         string `json:"image"`
	PullStartTime time.Time
	PullEndTime   time.Time
	PullLatency   int `json:"pullLatency"`
}

type imagePullMetric struct {
	Timestamp        time.Time                      `json:"timestamp"`
	PodName          string                         `json:"podName"`
	Namespace        string                         `json:"namespace"`
	MetricName       string                         `json:"metricName"`
	UUID             string                         `json:"uuid"`
	JobName          string                         `json:"jobName,omitempty"`
	JobIteration     int                            `json:"jobIteration"`
	Replica          int                            `json:"replica"`
	NodeName         string                         `json:"nodeName"`
	Metadata         any                            `json:"metadata,omitempty"`
	ContainerMetrics map[string]containerPullMetric `json:"containerMetrics"`
}

type imagePullLatency struct {
	BaseMeasurement
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
	}
}

func (ipl *imagePullLatency) handleCreatePod(obj any) {
	pod := obj.(*corev1.Pod)
	podLabels := pod.GetLabels()
	ipl.metrics.LoadOrStore(string(pod.UID), imagePullMetric{
		Timestamp:        pod.CreationTimestamp.Time.UTC(),
		PodName:          pod.Name,
		Namespace:        pod.Namespace,
		MetricName:       imagePullLatencyMeasurement,
		UUID:             ipl.Uuid,
		JobName:          ipl.JobConfig.Name,
		Metadata:         ipl.Metadata,
		JobIteration:     getIntFromLabels(podLabels, config.KubeBurnerLabelJobIteration),
		Replica:          getIntFromLabels(podLabels, config.KubeBurnerLabelReplica),
		NodeName:         pod.Spec.NodeName,
		ContainerMetrics: make(map[string]containerPullMetric),
	})
}

func (ipl *imagePullLatency) handleUpdatePod(obj any) {
	pod := obj.(*corev1.Pod)
	if value, exists := ipl.metrics.Load(string(pod.UID)); exists {
		ipm := value.(imagePullMetric)
		for _, containerStatus := range pod.Status.ContainerStatuses {
			containerName := containerStatus.Name
			containerMetric, exists := ipm.ContainerMetrics[containerName]
			if !exists {
				containerMetric = containerPullMetric{
					ContainerName: containerName,
					Image:         containerStatus.Image,
				}
			}

			// Handle container state transitions
			switch {
			case containerStatus.State.Waiting != nil:
				switch containerStatus.State.Waiting.Reason {
				case "Pulling":
					// Only set start time if not already set or if container is restarting
					if containerMetric.PullStartTime.IsZero() || containerStatus.RestartCount > 0 {
						containerMetric.PullStartTime = time.Now().UTC()
						// Reset end time and latency for restarts
						containerMetric.PullEndTime = time.Time{}
						containerMetric.PullLatency = 0
					}
				case "ImagePullBackOff", "ErrImagePull":
					// Handle failed pulls
					if containerMetric.PullStartTime.IsZero() {
						containerMetric.PullStartTime = time.Now().UTC()
					}
					containerMetric.PullEndTime = time.Now().UTC()
					containerMetric.PullLatency = -1 // Indicate failure
				}
			case containerStatus.State.Running != nil:
				// Only update if we have a start time and haven't recorded the end time
				if !containerMetric.PullStartTime.IsZero() && containerMetric.PullEndTime.IsZero() {
					containerMetric.PullEndTime = time.Now().UTC()
					containerMetric.PullLatency = int(containerMetric.PullEndTime.Sub(containerMetric.PullStartTime).Milliseconds())
				}
			}

			ipm.ContainerMetrics[containerName] = containerMetric
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
				name:          "imagePullPodWatcher",
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
	pods, err := ipl.ClientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
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
	var totalPulls int
	var failedPulls int
	ipl.metrics.Range(func(_, value any) bool {
		ipm := value.(imagePullMetric)
		for _, containerMetric := range ipm.ContainerMetrics {
			totalPulls++
			if containerMetric.PullLatency < 0 {
				failedPulls++
			}
		}
		return true
	})
	if totalPulls == 0 {
		return 0
	}
	// Calculate error percentage
	return float64(failedPulls) / float64(totalPulls) * 100
}

func (ipl *imagePullLatency) getLatency(normLatency any) map[string]float64 {
	ipm := normLatency.(imagePullMetric)
	latencies := make(map[string]float64)
	for containerName, containerMetric := range ipm.ContainerMetrics {
		// Only include successful pulls in latency metrics
		if containerMetric.PullLatency >= 0 {
			latencies[fmt.Sprintf("%s_pullLatency", containerName)] = float64(containerMetric.PullLatency)
		}
	}
	return latencies
}
