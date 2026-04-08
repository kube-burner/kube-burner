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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	lcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	podLatencyMeasurement          = "podLatencyMeasurement"
	podLatencyQuantilesMeasurement = "podLatencyQuantilesMeasurement"
	containersStarted              = "ContainersStarted"
)

var (
	supportedPodConditions = map[string]struct{}{
		string(corev1.ContainersReady): {},
		string(corev1.PodInitialized):  {},
		string(corev1.PodReady):        {},
		string(corev1.PodScheduled):    {},
	}
	supportedPodLatencyJobTypes = map[config.JobType]struct{}{
		config.CreationJob: {},
		config.PatchJob:    {},
		config.DeletionJob: {},
	}
)

type podLatencyLabels struct {
	JobIteration int    `json:"jobIteration"`
	Replica      int    `json:"replica"`
	Namespace    string `json:"namespace"`
	Name         string `json:"podName"`
	NodeName     string `json:"nodeName"`
}

type podMetric struct {
	metrics.LatencyDocument
	scheduled                     time.Time
	SchedulingLatency             int `json:"schedulingLatency"`
	initialized                   time.Time
	InitializedLatency            int `json:"initializedLatency"`
	containersReady               time.Time
	ContainersReadyLatency        int `json:"containersReadyLatency"`
	podReady                      time.Time
	PodReadyLatency               int `json:"podReadyLatency"`
	readyToStartContainers        time.Time
	ContainersStartedLatency      int `json:"containersStartedLatency"`
	containersStarted             time.Time
	ReadyToStartContainersLatency int              `json:"readyToStartContainersLatency"`
	PodLatencyLabels              podLatencyLabels `json:"labels"`
}

type podLatency struct {
	BaseMeasurement
	eventLister    lcorev1.EventLister
	stopInformerCh chan struct{}
}

type podLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newPodLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any, labelSelector string) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedPodConditions); err != nil {
		return nil, err
	}
	return podLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata, labelSelector),
	}, nil
}

func (plmf podLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &podLatency{
		BaseMeasurement: plmf.NewBaseLatency(jobConfig, clientSet, restConfig, podLatencyMeasurement, podLatencyQuantilesMeasurement, embedCfg),
	}
}

func (p *podLatency) handleCreatePod(obj any) {
	pod, err := util.ConvertAnyToTyped[corev1.Pod](obj)
	if err != nil {
		log.Errorf("failed to convert to Pod: %v", err)
		return
	}
	podLabels := pod.GetLabels()
	p.Metrics.LoadOrStore(string(pod.UID), podMetric{
		LatencyDocument: metrics.LatencyDocument{
			Timestamp:  pod.CreationTimestamp.UTC(),
			MetricName: podLatencyMeasurement,
			UUID:       p.Uuid,
			JobName:    p.JobConfig.Name,
			Metadata:   p.Metadata,
		},
		PodLatencyLabels: podLatencyLabels{
			Namespace:    pod.Namespace,
			Name:         pod.Name,
			NodeName:     pod.Spec.NodeName,
			JobIteration: getIntFromLabels(podLabels, config.KubeBurnerLabelJobIteration),
			Replica:      getIntFromLabels(podLabels, config.KubeBurnerLabelReplica),
		},
	})
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
			for _, c := range pod.Status.Conditions {
				if c.Status == corev1.ConditionTrue {
					// https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions
					switch c.Type {
					case corev1.PodScheduled:
						if pm.scheduled.IsZero() {
							pm.scheduled = p.getScheduledTimeFromEvent(pod)
							if pm.scheduled.IsZero() {
								pm.scheduled = c.LastTransitionTime.UTC()
							}
							pm.PodLatencyLabels.NodeName = pod.Spec.NodeName
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
						pm.containersStarted = p.getStartedTimeFromEvent(pod)
						pm.podReady = c.LastTransitionTime.UTC()
					}
				}
			}
			p.Metrics.Store(string(pod.UID), pm)
		}
	}
}

// getScheduledTimeFromEvent returns scheduled time in microseconds precission from event
func (p *podLatency) getScheduledTimeFromEvent(pod *corev1.Pod) time.Time {
	eventList, _ := p.eventLister.List(labels.Everything())
	for _, event := range eventList {
		if event.InvolvedObject.UID == pod.UID && event.Reason == "Scheduled" {
			return event.EventTime.Time
		}
	}
	return time.Time{}
}

// getStartedTimeFromEvent returns the timestamp of the latest container started event in the pod
func (p *podLatency) getStartedTimeFromEvent(pod *corev1.Pod) time.Time {
	var timestamp time.Time
	eventList, _ := p.eventLister.List(labels.Everything())
	for _, event := range eventList {
		if event.InvolvedObject.UID == pod.UID && event.Reason == "Started" {
			// The event name is in the format "podName.timestamp", where timestamp in hexadecimal format
			// https://github.com/kubernetes/client-go/blob/v0.34.2/tools/record/event.go#L492
			eventName := strings.Split(event.Name, ".")
			eventTsInt, err := strconv.ParseInt(eventName[1], 16, 64)
			if err != nil {
				log.Warnf("failed to parse event timestamp: %v", err)
				continue
			}
			eventTs := time.Unix(0, eventTsInt)
			// In pods with multiple containers, pick the timestamp of the latest "Started" event observed
			if timestamp.Before(eventTs) {
				timestamp = eventTs
			}
		}
	}
	return timestamp
}

// start podLatency measurement
func (p *podLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	gvr, err := util.ResourceToGVR(p.RestConfig, "Pod", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "Pod", err)
	}
	clientset := kubernetes.NewForConfigOrDie(p.RestConfig)
	lw := cache.NewFilteredListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		"events",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.FieldSelector = "involvedObject.kind=Pod"
		},
	)
	eventInformer := cache.NewSharedIndexInformer(lw, &corev1.Event{}, 0, cache.Indexers{})
	if err := eventInformer.SetTransform(eventTransformFunc()); err != nil {
		log.Warnf("failed to set event transform: %v", err)
	}
	p.stopInformerCh = make(chan struct{})
	go eventInformer.Run(p.stopInformerCh)
	if !cache.WaitForCacheSync(p.stopInformerCh, eventInformer.HasSynced) {
		return fmt.Errorf("failed to sync event informer cache")
	}
	p.eventLister = lcorev1.NewEventLister(eventInformer.GetIndexer())
	p.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(p.RestConfig),
				name:          "podWatcher",
				resource:      gvr,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: p.handleCreatePod,
					UpdateFunc: func(oldObj, newObj any) {
						p.handleUpdatePod(newObj)
					},
				},
				transform: PodTransformFunc(),
			},
		},
	)
	return nil
}

// collects pod measurements triggered in the past
func (p *podLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var pods []corev1.Pod
	options := metav1.ListOptions{
		LabelSelector: labels.Set(p.JobConfig.NamespaceLabels).String(),
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
			case corev1.PodReadyToStartContainers:
				readyToStartContainers = c.LastTransitionTime.UTC()
			case corev1.PodInitialized:
				initialized = c.LastTransitionTime.UTC()
			case corev1.ContainersReady:
				containersReady = c.LastTransitionTime.UTC()
			case corev1.PodReady:
				podReady = c.LastTransitionTime.UTC()
			}
		}
		p.Metrics.Store(string(pod.UID), podMetric{
			LatencyDocument: metrics.LatencyDocument{
				Timestamp:  pod.CreationTimestamp.UTC(),
				MetricName: podLatencyMeasurement,
				UUID:       p.Uuid,
				JobName:    p.JobConfig.Name,
				Metadata:   p.Metadata,
			},
			PodLatencyLabels: podLatencyLabels{
				Namespace:    pod.Namespace,
				Name:         pod.Name,
				NodeName:     pod.Spec.NodeName,
				JobIteration: getIntFromLabels(pod.Labels, config.KubeBurnerLabelJobIteration),
				Replica:      getIntFromLabels(pod.Labels, config.KubeBurnerLabelReplica),
			},
			scheduled:              scheduled,
			readyToStartContainers: readyToStartContainers,
			initialized:            initialized,
			containersReady:        containersReady,
			podReady:               podReady,
		})
	}
}

// Stop stops podLatency measurement
func (p *podLatency) Stop() error {
	close(p.stopInformerCh)
	return p.StopMeasurement(p.normalizeMetrics, p.getLatency)
}

func (p *podLatency) normalizeMetrics() float64 {
	totalPods := 0
	erroredPods := 0

	p.Metrics.Range(func(key, value any) bool {
		m := value.(podMetric)
		// If a pod does not reach the Running state (this timestamp isn't set), we skip that pod
		if m.podReady.IsZero() {
			log.Tracef("Pod %v latency ignored as it did not reach Ready state", m.PodLatencyLabels.Name)
			return true
		}
		// latencyTime should be always larger than zero, however, in some cases, it might be a
		// negative value due to the precision of timestamp can only get to the level of second
		errorFlag := 0
		m.ContainersReadyLatency = int(m.containersReady.Sub(m.Timestamp).Milliseconds())
		if m.ContainersReadyLatency < 0 {
			log.Tracef("ContainersReadyLatency for pod %v falling under negative case. So explicitly setting it to 0", m.PodLatencyLabels.Name)
			errorFlag = 1
			m.ContainersReadyLatency = 0
		}

		m.SchedulingLatency = int(m.scheduled.Sub(m.Timestamp).Milliseconds())
		if m.SchedulingLatency < 0 {
			log.Tracef("SchedulingLatency for pod %v falling under negative case. So explicitly setting it to 0", m.PodLatencyLabels.Name)
			errorFlag = 1
			m.SchedulingLatency = 0
		}

		m.ReadyToStartContainersLatency = int(m.readyToStartContainers.Sub(m.Timestamp).Milliseconds())
		if m.ReadyToStartContainersLatency < 0 {
			log.Tracef("ReadyToStartContainersLatency for pod %v falling under negative case. So explicitly setting it to 0", m.PodLatencyLabels.Name)
			errorFlag = 1
			m.ReadyToStartContainersLatency = 0
		}

		m.InitializedLatency = int(m.initialized.Sub(m.Timestamp).Milliseconds())
		if m.InitializedLatency < 0 {
			log.Tracef("InitializedLatency for pod %v falling under negative case. So explicitly setting it to 0", m.PodLatencyLabels.Name)
			errorFlag = 1
			m.InitializedLatency = 0
		}

		m.PodReadyLatency = int(m.podReady.Sub(m.Timestamp).Milliseconds())
		if m.PodReadyLatency < 0 {
			log.Tracef("PodReadyLatency for pod %v falling under negative case. So explicitly setting it to 0", m.PodLatencyLabels.Name)
			errorFlag = 1
			m.PodReadyLatency = 0
		}
		m.ContainersStartedLatency = int(m.containersStarted.Sub(m.Timestamp).Milliseconds())
		if m.ContainersStartedLatency < 0 {
			log.Tracef("ContainersStartedLatency for pod %v falling under negative case. So explicitly setting it to 0", m.PodLatencyLabels.Name)
			errorFlag = 1
			m.ContainersStartedLatency = 0
		}
		m.ChurnMetric = p.IsChurnMetric(m.Timestamp)
		totalPods++
		erroredPods += errorFlag
		baseLabels := podLatencyLabels{
			Namespace:    m.PodLatencyLabels.Namespace,
			Name:         m.PodLatencyLabels.Name,
			NodeName:     m.PodLatencyLabels.NodeName,
			JobIteration: m.PodLatencyLabels.JobIteration,
			Replica:      m.PodLatencyLabels.Replica,
		}
		makeDoc := func(condition string, valueMs int) metrics.LatencyDocument {
			var lbls map[string]string
			b, _ := json.Marshal(baseLabels)
			_ = json.Unmarshal(b, &lbls)
			lbls["condition"] = condition
			return metrics.LatencyDocument{
				Timestamp:  m.Timestamp,
				Labels:     lbls,
				Value:      float64(valueMs),
				MetricName: podLatencyMeasurement,
				UUID:       p.Uuid,
				JobName:    p.JobConfig.Name,
				Metadata:   p.Metadata,
			}
		}
		p.NormLatencies = append(p.NormLatencies,
			makeDoc(string(corev1.PodScheduled), m.SchedulingLatency),
			makeDoc(string(corev1.PodInitialized), m.InitializedLatency),
			makeDoc(string(corev1.ContainersReady), m.ContainersReadyLatency),
			makeDoc(string(corev1.PodReady), m.PodReadyLatency),
			makeDoc(string(corev1.PodReadyToStartContainers), m.ReadyToStartContainersLatency),
			makeDoc(containersStarted, m.ContainersStartedLatency),
		)
		return true
	})
	if totalPods == 0 {
		return 0.0
	}
	return float64(erroredPods) / float64(totalPods) * 100.0
}

func (p *podLatency) getLatency(normLatency any) map[string]float64 {
	podMetric := normLatency.(metrics.LatencyDocument)
	condition := podMetric.Labels["condition"]
	return map[string]float64{condition: podMetric.Value}
}

func (p *podLatency) IsCompatible() bool {
	_, exists := supportedPodLatencyJobTypes[p.JobConfig.JobType]
	return exists
}

// eventTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid
// - involvedObject: uid
// - reason, eventTime
func eventTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		event, ok := obj.(*corev1.Event)
		if !ok {
			return obj, nil
		}

		// Create minimal event with only fields needed for latency measurement
		minimal := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      event.Name,
				Namespace: event.Namespace,
				UID:       event.UID,
			},
			InvolvedObject: corev1.ObjectReference{
				UID: event.InvolvedObject.UID,
			},
			Reason:    event.Reason,
			EventTime: event.EventTime,
		}

		return minimal, nil
	}
}
