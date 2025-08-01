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
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
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
	// High-precision client-side timestamps
	clientScheduled              time.Time
	clientInitialized            time.Time
	clientContainersReady        time.Time
	clientPodReady               time.Time
	clientReadyToStartContainers time.Time
	// High-precision latencies (in milliseconds with sub-millisecond precision)
	ClientSchedulingLatency             float64 `json:"clientSchedulingLatency,omitempty"`
	ClientInitializedLatency            float64 `json:"clientInitializedLatency,omitempty"`
	ClientContainersReadyLatency        float64 `json:"clientContainersReadyLatency,omitempty"`
	ClientPodReadyLatency               float64 `json:"clientPodReadyLatency,omitempty"`
	ClientReadyToStartContainersLatency float64 `json:"clientReadyToStartContainersLatency,omitempty"`
}

type podLatency struct {
	BaseMeasurement
	highPrecisionMetrics bool
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
		BaseMeasurement:      plmf.NewBaseLatency(jobConfig, clientSet, restConfig, podLatencyMeasurement, podLatencyQuantilesMeasurement, embedCfg),
		highPrecisionMetrics: plmf.Config.HighPrecisionMetrics,
	}
}

func (p *podLatency) handleCreatePod(obj any) {
	pod, err := util.ConvertAnyToTyped[corev1.Pod](obj)
	if err != nil {
		log.Errorf("failed to convert to Pod: %v", err)
		return
	}
	podLabels := pod.GetLabels()
	clientTimestamp := time.Now().UTC()

	pm := podMetric{
		Timestamp:    pod.CreationTimestamp.UTC(),
		Namespace:    pod.Namespace,
		Name:         pod.Name,
		MetricName:   podLatencyMeasurement,
		UUID:         p.Uuid,
		JobName:      p.JobConfig.Name,
		Metadata:     p.Metadata,
		JobIteration: getIntFromLabels(podLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(podLabels, config.KubeBurnerLabelReplica),
	}

	if p.highPrecisionMetrics {
		pm.Timestamp = clientTimestamp
		log.Debugf("High-precision mode: Using client timestamp %v for pod %s", clientTimestamp, pod.Name)
	}

	p.Metrics.LoadOrStore(string(pod.UID), pm)
}

func (p *podLatency) handleUpdatePod(obj any) {
	pod, err := util.ConvertAnyToTyped[corev1.Pod](obj)
	if err != nil {
		log.Errorf("failed to convert to Pod: %v", err)
		return
	}
	if value, exists := p.Metrics.Load(string(pod.UID)); exists {
		pm := value.(podMetric)
		if pm.podReady.IsZero() {
			clientTimestamp := time.Now().UTC()

			for _, c := range pod.Status.Conditions {
				if c.Status == corev1.ConditionTrue {
					// https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions
					switch c.Type {
					case corev1.PodScheduled:
						if pm.scheduled.IsZero() {
							pm.scheduled = c.LastTransitionTime.UTC()
							pm.NodeName = pod.Spec.NodeName
							if p.highPrecisionMetrics && pm.clientScheduled.IsZero() {
								pm.clientScheduled = clientTimestamp
								log.Debugf("High-precision: Pod %s scheduled at client time %v", pod.Name, clientTimestamp)
							}
						}
					case corev1.PodReadyToStartContainers:
						if pm.readyToStartContainers.IsZero() {
							pm.readyToStartContainers = c.LastTransitionTime.UTC()
							if p.highPrecisionMetrics && pm.clientReadyToStartContainers.IsZero() {
								pm.clientReadyToStartContainers = clientTimestamp
							}
						}
					case corev1.PodInitialized:
						if pm.initialized.IsZero() {
							pm.initialized = c.LastTransitionTime.UTC()
							if p.highPrecisionMetrics && pm.clientInitialized.IsZero() {
								pm.clientInitialized = clientTimestamp
							}
						}
					case corev1.ContainersReady:
						if pm.containersReady.IsZero() {
							pm.containersReady = c.LastTransitionTime.UTC()
							if p.highPrecisionMetrics && pm.clientContainersReady.IsZero() {
								pm.clientContainersReady = clientTimestamp
							}
						}
					case corev1.PodReady:
						log.Debugf("Pod %s is ready", pod.Name)
						pm.podReady = c.LastTransitionTime.UTC()
						if p.highPrecisionMetrics && pm.clientPodReady.IsZero() {
							pm.clientPodReady = clientTimestamp
							log.Debugf("High-precision: Pod %s ready at client time %v", pod.Name, clientTimestamp)
						}
					}
				}
			}
			p.Metrics.Store(string(pod.UID), pm)
		}
	}
}

// start podLatency measurement
func (p *podLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	gvr, err := util.ResourceToGVR(p.RestConfig, "Pod", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "Pod", err)
	}
	p.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(p.RestConfig),
				name:          "podWatcher",
				resource:      gvr,
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
	p.Metrics = sync.Map{}
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
		p.Metrics.Store(string(pod.UID), podMetric{
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

	p.Metrics.Range(func(key, value any) bool {
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

		// Calculate standard API-based latencies (milliseconds)
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

		// Calculate high-precision client-side latencies (milliseconds with sub-millisecond precision) if enabled
		if p.highPrecisionMetrics {
			if !m.clientScheduled.IsZero() {
				m.ClientSchedulingLatency = float64(m.clientScheduled.Sub(m.Timestamp).Nanoseconds()) / 1e6
				if m.ClientSchedulingLatency < 0 {
					m.ClientSchedulingLatency = 0
				}
			}

			if !m.clientInitialized.IsZero() {
				m.ClientInitializedLatency = float64(m.clientInitialized.Sub(m.Timestamp).Nanoseconds()) / 1e6
				if m.ClientInitializedLatency < 0 {
					m.ClientInitializedLatency = 0
				}
			}

			if !m.clientContainersReady.IsZero() {
				m.ClientContainersReadyLatency = float64(m.clientContainersReady.Sub(m.Timestamp).Nanoseconds()) / 1e6
				if m.ClientContainersReadyLatency < 0 {
					m.ClientContainersReadyLatency = 0
				}
			}

			if !m.clientPodReady.IsZero() {
				m.ClientPodReadyLatency = float64(m.clientPodReady.Sub(m.Timestamp).Nanoseconds()) / 1e6
				if m.ClientPodReadyLatency < 0 {
					m.ClientPodReadyLatency = 0
				}
			}

			if !m.clientReadyToStartContainers.IsZero() {
				m.ClientReadyToStartContainersLatency = float64(m.clientReadyToStartContainers.Sub(m.Timestamp).Nanoseconds()) / 1e6
				if m.ClientReadyToStartContainersLatency < 0 {
					m.ClientReadyToStartContainersLatency = 0
				}
			}

			log.Debugf("High-precision latencies for pod %s: Scheduling=%.3fms, Ready=%.3fms",
				m.Name, m.ClientSchedulingLatency, m.ClientPodReadyLatency)
		}

		totalPods++
		erroredPods += errorFlag
		p.NormLatencies = append(p.NormLatencies, m)
		return true
	})
	if totalPods == 0 {
		return 0.0
	}
	return float64(erroredPods) / float64(totalPods) * 100.0
}

func (p *podLatency) getLatency(normLatency any) map[string]float64 {
	podMetric := normLatency.(podMetric)
	latencies := map[string]float64{
		string(corev1.PodScheduled):              float64(podMetric.SchedulingLatency),
		string(corev1.ContainersReady):           float64(podMetric.ContainersReadyLatency),
		string(corev1.PodInitialized):            float64(podMetric.InitializedLatency),
		string(corev1.PodReady):                  float64(podMetric.PodReadyLatency),
		string(corev1.PodReadyToStartContainers): float64(podMetric.ReadyToStartContainersLatency),
	}

	// Add high-precision client-side latencies if enabled
	if p.highPrecisionMetrics {
		latencies["ClientPodScheduled"] = float64(podMetric.ClientSchedulingLatency)
		latencies["ClientContainersReady"] = float64(podMetric.ClientContainersReadyLatency)
		latencies["ClientPodInitialized"] = float64(podMetric.ClientInitializedLatency)
		latencies["ClientPodReady"] = float64(podMetric.ClientPodReadyLatency)
		latencies["ClientPodReadyToStartContainers"] = float64(podMetric.ClientReadyToStartContainersLatency)
	}

	return latencies
}
