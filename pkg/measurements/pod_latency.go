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

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	podLatencyMeasurement          = "podLatencyMeasurement"
	podLatencyQuantilesMeasurement = "podLatencyQuantilesMeasurement"
)

type podMetric struct {
	Timestamp              time.Time `json:"timestamp"`
	scheduled              time.Time
	SchedulingLatency      int `json:"schedulingLatency"`
	initialized            time.Time
	InitializedLatency     int `json:"initializedLatency"`
	containersReady        time.Time
	ContainersReadyLatency int `json:"containersReadyLatency"`
	podReady               time.Time
	PodReadyLatency        int         `json:"podReadyLatency"`
	MetricName             string      `json:"metricName"`
	JobConfig              config.Job  `json:"jobConfig"`
	UUID                   string      `json:"uuid"`
	Namespace              string      `json:"namespace"`
	Name                   string      `json:"podName"`
	NodeName               string      `json:"nodeName"`
	Metadata               interface{} `json:"metadata,omitempty"`
}

type podLatency struct {
	config           types.Measurement
	watcher          *metrics.Watcher
	metrics          map[string]podMetric
	metricLock       sync.RWMutex
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

func init() {
	measurementMap["podLatency"] = &podLatency{
		metrics: make(map[string]podMetric),
	}
}

func (p *podLatency) handleCreatePod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	p.metricLock.Lock()
	defer p.metricLock.Unlock()
	if _, exists := p.metrics[string(pod.UID)]; !exists {
		p.metrics[string(pod.UID)] = podMetric{
			Timestamp:  pod.CreationTimestamp.Time.UTC(),
			Namespace:  pod.Namespace,
			Name:       pod.Name,
			MetricName: podLatencyMeasurement,
			UUID:       globalCfg.UUID,
			JobConfig:  *factory.jobConfig,
			Metadata:   factory.metadata,
		}
	}
}

func (p *podLatency) handleUpdatePod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	p.metricLock.Lock()
	defer p.metricLock.Unlock()
	if pm, exists := p.metrics[string(pod.UID)]; exists && pm.podReady.IsZero() {
		for _, c := range pod.Status.Conditions {
			if c.Status == corev1.ConditionTrue {
				switch c.Type {
				case corev1.PodScheduled:
					if pm.scheduled.IsZero() {
						pm.scheduled = c.LastTransitionTime.Time.UTC()
						pm.NodeName = pod.Spec.NodeName
					}
				case corev1.PodInitialized:
					if pm.initialized.IsZero() {
						pm.initialized = c.LastTransitionTime.Time.UTC()
					}
				case corev1.ContainersReady:
					if pm.containersReady.IsZero() {
						pm.containersReady = c.LastTransitionTime.Time.UTC()
					}
				case corev1.PodReady:
					if pm.podReady.IsZero() {
						log.Debugf("Pod %s is ready", pod.Name)
						pm.podReady = c.LastTransitionTime.Time.UTC()
					}
				}
			}
		}
		p.metrics[string(pod.UID)] = pm
	}
}

func (p *podLatency) setConfig(cfg types.Measurement) {
	p.config = cfg
}

func (p *podLatency) validateConfig() error {
	var metricFound bool
	var latencyMetrics = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range p.config.LatencyThresholds {
		if th.ConditionType == string(corev1.ContainersReady) || th.ConditionType == string(corev1.PodInitialized) || th.ConditionType == string(corev1.PodReady) || th.ConditionType == string(corev1.PodScheduled) {
			for _, lm := range latencyMetrics {
				if th.Metric == lm {
					metricFound = true
					break
				}
			}
			if !metricFound {
				return fmt.Errorf("unsupported metric %s in podLatency measurement, supported are: %s", th.Metric, strings.Join(latencyMetrics, ", "))
			}
		} else {
			return fmt.Errorf("unsupported pod condition type in podLatency measurement: %s", th.ConditionType)
		}
	}
	return nil
}

// start podLatency measurement
func (p *podLatency) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Reset latency slices, required in multi-job benchmarks
	p.latencyQuantiles, p.normLatencies = nil, nil
	p.metrics = make(map[string]podMetric)
	log.Infof("Creating Pod latency watcher for %s", factory.jobConfig.Name)
	p.watcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"podWatcher",
		"pods",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", globalCfg.RUNID)
		},
		nil,
	)
	p.watcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: p.handleCreatePod,
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.handleUpdatePod(newObj)
		},
	})
	if err := p.watcher.StartAndCacheSync(); err != nil {
		log.Errorf("Pod Latency measurement error: %s", err)
	}
	return nil
}

// collects pod measurements triggered in the past
func (p *podLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var pods []corev1.Pod
	labelSelector := labels.SelectorFromSet(factory.jobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	namespaces := strings.Split(factory.jobConfig.Namespace, ",")
	for _, namespace := range namespaces {
		podList, err := factory.clientSet.CoreV1().Pods(namespace).List(context.TODO(), options)
		if err != nil {
			log.Errorf("error listing pods in namespace %s: %v", namespace, err)
		}
		pods = append(pods, podList.Items...)
	}
	p.metrics = make(map[string]podMetric)
	for _, pod := range pods {
		var scheduled, initialized, containersReady, podReady time.Time
		for _, c := range pod.Status.Conditions {
			switch c.Type {
			case corev1.PodScheduled:
				scheduled = c.LastTransitionTime.Time.UTC()
			case corev1.PodInitialized:
				initialized = c.LastTransitionTime.Time.UTC()
			case corev1.ContainersReady:
				containersReady = c.LastTransitionTime.Time.UTC()
			case corev1.PodReady:
				podReady = c.LastTransitionTime.Time.UTC()
			}
		}
		p.metrics[string(pod.UID)] = podMetric{
			Timestamp:       pod.Status.StartTime.Time.UTC(),
			Namespace:       pod.Namespace,
			Name:            pod.Name,
			MetricName:      podLatencyMeasurement,
			NodeName:        pod.Spec.NodeName,
			UUID:            globalCfg.UUID,
			JobConfig:       *factory.jobConfig,
			Metadata:        factory.metadata,
			scheduled:       scheduled,
			initialized:     initialized,
			containersReady: containersReady,
			podReady:        podReady,
		}
	}
}

// Stop stops podLatency measurement
func (p *podLatency) stop() error {
	var err error
	if p.watcher != nil {
		p.watcher.StopWatcher()
	}
	errorRate := p.normalizeMetrics()
	if errorRate > 10.00 {
		log.Error("Latency errors beyond 10%. Hence invalidating the results")
		return fmt.Errorf("Something is wrong with system under test. Pod latencies error rate was: %.2f", errorRate)
	}
	p.calcQuantiles()
	if len(p.config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(p.config.LatencyThresholds, p.latencyQuantiles)
	}
	for _, q := range p.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %s 50th: %v 99th: %v max: %v avg: %v", factory.jobConfig.Name, pq.QuantileName, pq.P50, pq.P99, pq.Max, pq.Avg)
	}
	if errorRate > 0 {
		log.Infof("Pod latencies error rate was: %.2f", errorRate)
	}
	return err
}

// index sends metrics to the configured indexer
func (p *podLatency) index(indexer indexers.Indexer, jobName string) {
	log.Infof("Indexing pod latency data for job: %s", jobName)
	metricMap := map[string][]interface{}{
		podLatencyMeasurement:          p.normLatencies,
		podLatencyQuantilesMeasurement: p.latencyQuantiles,
	}
	for metricName, data := range metricMap {
		indexingOpts := indexers.IndexingOpts{
			MetricName: fmt.Sprintf("%s-%s", metricName, jobName),
		}
		log.Debugf("Indexing [%d] documents: %s", len(data), metricName)
		resp, err := indexer.Index(data, indexingOpts)
		if err != nil {
			log.Error(err.Error())
		} else {
			log.Info(resp)
		}
	}
}

func (p *podLatency) normalizeMetrics() float64 {
	totalPods := 0
	erroredPods := 0
	for _, m := range p.metrics {
		// If a pod does not reach the Running state (this timestamp isn't set), we skip that pod
		if m.podReady.IsZero() {
			log.Tracef("Pod %v latency ignored as it did not reach Ready state", m.Name)
			continue
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
	}
	if totalPods == 0 {
		return 0.0
	}
	return float64(erroredPods) / float64(totalPods) * 100.0
}

func (p *podLatency) calcQuantiles() {
	quantileMap := map[corev1.PodConditionType][]float64{}
	for _, normLatency := range p.normLatencies {
		quantileMap[corev1.PodScheduled] = append(quantileMap[corev1.PodScheduled], float64(normLatency.(podMetric).SchedulingLatency))
		quantileMap[corev1.ContainersReady] = append(quantileMap[corev1.ContainersReady], float64(normLatency.(podMetric).ContainersReadyLatency))
		quantileMap[corev1.PodInitialized] = append(quantileMap[corev1.PodInitialized], float64(normLatency.(podMetric).InitializedLatency))
		quantileMap[corev1.PodReady] = append(quantileMap[corev1.PodReady], float64(normLatency.(podMetric).PodReadyLatency))
	}
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = globalCfg.UUID
		latencySummary.JobConfig = *factory.jobConfig
		latencySummary.Metadata = factory.metadata
		latencySummary.MetricName = podLatencyQuantilesMeasurement
		return latencySummary
	}
	for podCondition, latencies := range quantileMap {
		p.latencyQuantiles = append(p.latencyQuantiles, calcSummary(string(podCondition), latencies))
	}
}
