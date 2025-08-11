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

const (
	// Client-side high-precision condition names
	ClientContainersReady           = "ClientContainersReady"
	ClientPodInitialized            = "ClientPodInitialized"
	ClientPodReady                  = "ClientPodReady"
	ClientPodScheduled              = "ClientPodScheduled"
	ClientPodReadyToStartContainers = "ClientPodReadyToStartContainers"
)

var (
	supportedPodConditions = map[string]struct{}{
		string(corev1.ContainersReady): {},
		string(corev1.PodInitialized):  {},
		string(corev1.PodReady):        {},
		string(corev1.PodScheduled):    {},
		// Client-side high-precision conditions
		ClientContainersReady:           {},
		ClientPodInitialized:            {},
		ClientPodReady:                  {},
		ClientPodScheduled:              {},
		ClientPodReadyToStartContainers: {},
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
	clientCreationTimestamp      time.Time // Client-side creation timestamp for high-precision base
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
		Timestamp:               pod.CreationTimestamp.UTC(), // Use API timestamp for server-side calculations
		clientCreationTimestamp: clientTimestamp,             // Store client-side creation timestamp for high-precision calculations
		Namespace:               pod.Namespace,
		Name:                    pod.Name,
		MetricName:              podLatencyMeasurement,
		UUID:                    p.Uuid,
		JobName:                 p.JobConfig.Name,
		Metadata:                p.Metadata,
		JobIteration:            getIntFromLabels(podLabels, config.KubeBurnerLabelJobIteration),
		Replica:                 getIntFromLabels(podLabels, config.KubeBurnerLabelReplica),
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
							pm.clientScheduled = clientTimestamp
						}
					case corev1.PodReadyToStartContainers:
						if pm.readyToStartContainers.IsZero() {
							pm.readyToStartContainers = c.LastTransitionTime.UTC()
							pm.clientReadyToStartContainers = clientTimestamp
						}
					case corev1.PodInitialized:
						if pm.initialized.IsZero() {
							pm.initialized = c.LastTransitionTime.UTC()
							pm.clientInitialized = clientTimestamp
						}
					case corev1.ContainersReady:
						if pm.containersReady.IsZero() {
							pm.containersReady = c.LastTransitionTime.UTC()
							pm.clientContainersReady = clientTimestamp
						}
					case corev1.PodReady:
						log.Debugf("Pod %s is ready", pod.Name)
						pm.podReady = c.LastTransitionTime.UTC()
						pm.clientPodReady = clientTimestamp
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
		var scheduled, initialized, containersReady, podReady, readyToStartContainers time.Time
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
			case corev1.PodReadyToStartContainers:
				readyToStartContainers = c.LastTransitionTime.UTC()
			}
		}

		baseTimestamp := pod.CreationTimestamp.UTC()
		if pod.Status.StartTime != nil {
			baseTimestamp = pod.Status.StartTime.UTC()
		}

		p.Metrics.Store(string(pod.UID), podMetric{
			Timestamp:              baseTimestamp,
			Namespace:              pod.Namespace,
			Name:                   pod.Name,
			MetricName:             podLatencyMeasurement,
			NodeName:               pod.Spec.NodeName,
			UUID:                   p.Uuid,
			scheduled:              scheduled,
			initialized:            initialized,
			containersReady:        containersReady,
			podReady:               podReady,
			readyToStartContainers: readyToStartContainers,
			JobName:                p.JobConfig.Name,
			// Client-side timestamps are not available for collected metrics
			// as they were not captured in real-time
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

		m.ReadyToStartContainersLatency = int(m.readyToStartContainers.Sub(m.Timestamp).Milliseconds())
		if m.ReadyToStartContainersLatency < 0 {
			log.Tracef("ReadyToStartContainersLatency for pod %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.ReadyToStartContainersLatency = 0
		}

		// Calculate high-precision client-side latencies only when we have client-side timestamps
		if !m.clientCreationTimestamp.IsZero() {
			if !m.clientScheduled.IsZero() {
				m.ClientSchedulingLatency = float64(m.clientScheduled.Sub(m.clientCreationTimestamp).Nanoseconds()) / 1e6
				if m.ClientSchedulingLatency < 0 {
					m.ClientSchedulingLatency = 0
				}
			}

			if !m.clientInitialized.IsZero() {
				m.ClientInitializedLatency = float64(m.clientInitialized.Sub(m.clientCreationTimestamp).Nanoseconds()) / 1e6
				if m.ClientInitializedLatency < 0 {
					m.ClientInitializedLatency = 0
				}
			}

			if !m.clientContainersReady.IsZero() {
				m.ClientContainersReadyLatency = float64(m.clientContainersReady.Sub(m.clientCreationTimestamp).Nanoseconds()) / 1e6
				if m.ClientContainersReadyLatency < 0 {
					m.ClientContainersReadyLatency = 0
				}
			}

			if !m.clientPodReady.IsZero() {
				m.ClientPodReadyLatency = float64(m.clientPodReady.Sub(m.clientCreationTimestamp).Nanoseconds()) / 1e6
				if m.ClientPodReadyLatency < 0 {
					m.ClientPodReadyLatency = 0
				}
			}

			if !m.clientReadyToStartContainers.IsZero() {
				m.ClientReadyToStartContainersLatency = float64(m.clientReadyToStartContainers.Sub(m.clientCreationTimestamp).Nanoseconds()) / 1e6
				if m.ClientReadyToStartContainersLatency < 0 {
					m.ClientReadyToStartContainersLatency = 0
				}
			}
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

	// Only report high-precision client-side latencies when highPrecisionMetrics flag is enabled
	if p.highPrecisionMetrics {
		latencies[ClientPodScheduled] = podMetric.ClientSchedulingLatency
		latencies[ClientContainersReady] = podMetric.ClientContainersReadyLatency
		latencies[ClientPodInitialized] = podMetric.ClientInitializedLatency
		latencies[ClientPodReady] = podMetric.ClientPodReadyLatency
		latencies[ClientPodReadyToStartContainers] = podMetric.ClientReadyToStartContainersLatency
	}

	return latencies
}
