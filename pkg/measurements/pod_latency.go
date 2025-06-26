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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	podLatencyMeasurement          = "podLatencyMeasurement"
	podLatencyQuantilesMeasurement = "podLatencyQuantilesMeasurement"
)

var (
	supportedPodConditions = map[string]struct{}{
		string(corev1.ContainersReady): {},
		string(corev1.PodInitialized):  {},
		string(corev1.PodReady):        {},
		string(corev1.PodScheduled):    {},
	}
)

type podMetric struct {
	Timestamp                     time.Time `json:"timestamp"`
	scheduled                     time.Time
	SchedulingLatency             int `json:"schedulingLatency"`
	initialized                   time.Time
	InitializedLatency            int `json:"initializedLatency"`
	containersReady               time.Time
	ContainersReadyLatency        int `json:"containersReadyLatency"`
	podReady                      time.Time
	PodReadyLatency               int `json:"podReadyLatency"`
	readyToStartContainers        time.Time
	ReadyToStartContainersLatency int    `json:"readyToStartContainersLatency"`
	MetricName                    string `json:"metricName"`
	UUID                          string `json:"uuid"`
	JobName                       string `json:"jobName,omitempty"`
	JobIteration                  int    `json:"jobIteration"`
	Replica                       int    `json:"replica"`
	Namespace                     string `json:"namespace"`
	Name                          string `json:"podName"`
	NodeName                      string `json:"nodeName"`
	Metadata                      any    `json:"metadata,omitempty"`
}

type podLatency struct {
	BaseMeasurement
}

type podLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newPodLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedPodConditions); err != nil {
		return nil, err
	}
	return podLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (plmf podLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &podLatency{
		BaseMeasurement: plmf.NewBaseLatency(jobConfig, clientSet, restConfig, podLatencyMeasurement, podLatencyQuantilesMeasurement, embedCfg),
	}
}

func (p *podLatency) handleCreatePod(obj any) {
	pod := obj.(*corev1.Pod)
	podLabels := pod.GetLabels()
	p.metrics.LoadOrStore(string(pod.UID), podMetric{
		Timestamp:    pod.CreationTimestamp.UTC(),
		Namespace:    pod.Namespace,
		Name:         pod.Name,
		MetricName:   podLatencyMeasurement,
		UUID:         p.Uuid,
		JobName:      p.JobConfig.Name,
		Metadata:     p.Metadata,
		JobIteration: getIntFromLabels(podLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(podLabels, config.KubeBurnerLabelReplica),
	})
}

func (p *podLatency) handleUpdatePod(obj any) {
	pod := obj.(*corev1.Pod)
	if value, exists := p.metrics.Load(string(pod.UID)); exists {
		pm := value.(podMetric)
		if pm.podReady.IsZero() {
			for _, c := range pod.Status.Conditions {
				if c.Status == corev1.ConditionTrue {
					// https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions
					switch c.Type {
					case corev1.PodScheduled:
						if pm.scheduled.IsZero() {
							pm.scheduled = c.LastTransitionTime.UTC()
							pm.NodeName = pod.Spec.NodeName
						}
					case corev1.PodReadyToStartContainers:
						if pm.readyToStartContainers.IsZero() {
							pm.readyToStartContainers = c.LastTransitionTime.UTC()
						}
					case corev1.PodInitialized:
						if pm.initialized.IsZero() {
							pm.initialized = c.LastTransitionTime.UTC()
						}
					case corev1.ContainersReady:
						if pm.containersReady.IsZero() {
							pm.containersReady = c.LastTransitionTime.UTC()
						}
					case corev1.PodReady:
						log.Debugf("Pod %s is ready", pod.Name)
						pm.podReady = c.LastTransitionTime.UTC()
					}
				}
			}
			p.metrics.Store(string(pod.UID), pm)
		}
	}
}

// start podLatency measurement
func (p *podLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	p.startMeasurement(
		[]MeasurementWatcher{
			{
				restClient:    p.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
				name:          "podWatcher",
				resource:      "pods",
				labelSelector: fmt.Sprintf("kube-burner-runid=%v", p.Runid),
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: p.handleCreatePod,
					UpdateFunc: func(oldObj, newObj any) {
						p.handleUpdatePod(newObj)
					},
				},
			},
		},
	)
	return nil
}

// collects pod measurements triggered in the past
func (p *podLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var pods []corev1.Pod
	labelSelector := labels.SelectorFromSet(p.JobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	namespaces := strings.Split(p.JobConfig.Namespace, ",")
	for _, namespace := range namespaces {
		podList, err := p.ClientSet.CoreV1().Pods(namespace).List(context.TODO(), options)
		if err != nil {
			log.Errorf("error listing pods in namespace %s: %v", namespace, err)
		}
		pods = append(pods, podList.Items...)
	}
	p.metrics = sync.Map{}
	for _, pod := range pods {
		var scheduled, initialized, containersReady, podReady time.Time
		for _, c := range pod.Status.Conditions {
			switch c.Type {
			case corev1.PodScheduled:
				scheduled = c.LastTransitionTime.UTC()
			case corev1.PodInitialized:
				initialized = c.LastTransitionTime.UTC()
			case corev1.ContainersReady:
				containersReady = c.LastTransitionTime.UTC()
			case corev1.PodReady:
				podReady = c.LastTransitionTime.UTC()
			}
		}
		p.metrics.Store(string(pod.UID), podMetric{
			Timestamp:       pod.Status.StartTime.UTC(),
			Namespace:       pod.Namespace,
			Name:            pod.Name,
			MetricName:      podLatencyMeasurement,
			NodeName:        pod.Spec.NodeName,
			UUID:            p.Uuid,
			scheduled:       scheduled,
			initialized:     initialized,
			containersReady: containersReady,
			podReady:        podReady,
			JobName:         p.JobConfig.Name,
		})
	}
}

// Stop stops podLatency measurement
func (p *podLatency) Stop() error {
	return p.StopMeasurement(p.normalizeMetrics, p.getLatency)
}

func (p *podLatency) normalizeMetrics() float64 {
	totalPods := 0
	erroredPods := 0

	p.metrics.Range(func(key, value any) bool {
		m := value.(podMetric)
		// If a pod does not reach the Running state (this timestamp isn't set), we skip that pod
		if m.podReady.IsZero() {
			log.Tracef("Pod %v latency ignored as it did not reach Ready state", m.Name)
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
		m.ContainersReadyLatency = int(m.containersReady.Sub(m.Timestamp).Milliseconds())
		if m.ContainersReadyLatency < 0 {
			log.Tracef("ContainersReadyLatency for pod %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.ContainersReadyLatency = 0
		}

		m.SchedulingLatency = int(m.scheduled.Sub(m.Timestamp).Milliseconds())
		if m.SchedulingLatency < 0 {
			log.Tracef("SchedulingLatency for pod %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.SchedulingLatency = 0
		}

		m.InitializedLatency = int(m.initialized.Sub(m.Timestamp).Milliseconds())
		if m.InitializedLatency < 0 {
			log.Tracef("InitializedLatency for pod %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.InitializedLatency = 0
		}

		m.PodReadyLatency = int(m.podReady.Sub(m.Timestamp).Milliseconds())
		if m.PodReadyLatency < 0 {
			log.Tracef("PodReadyLatency for pod %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.PodReadyLatency = 0
		}
		totalPods++
		erroredPods += errorFlag
		p.normLatencies = append(p.normLatencies, m)
		return true
	})
	if totalPods == 0 {
		return 0.0
	}
	return float64(erroredPods) / float64(totalPods) * 100.0
}

func (p *podLatency) getLatency(normLatency any) map[string]float64 {
	podMetric := normLatency.(podMetric)
	return map[string]float64{
		string(corev1.PodScheduled):              float64(podMetric.SchedulingLatency),
		string(corev1.ContainersReady):           float64(podMetric.ContainersReadyLatency),
		string(corev1.PodInitialized):            float64(podMetric.InitializedLatency),
		string(corev1.PodReady):                  float64(podMetric.PodReadyLatency),
		string(corev1.PodReadyToStartContainers): float64(podMetric.ReadyToStartContainersLatency),
	}
}
