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
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/metrics"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
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
	JobName                string      `json:"jobName"`
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
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

func init() {
	measurementMap["podLatency"] = &podLatency{}
}

func (p *podLatency) handleCreatePod(obj interface{}) {
	now := time.Now().UTC()
	pod := obj.(*v1.Pod)
	jobConfig := *factory.jobConfig
	if _, exists := p.metrics[string(pod.UID)]; !exists {
		if strings.Contains(pod.Namespace, factory.jobConfig.Namespace) {
			p.metrics[string(pod.UID)] = podMetric{
				Timestamp:  now,
				Namespace:  pod.Namespace,
				Name:       pod.Name,
				MetricName: podLatencyMeasurement,
				UUID:       globalCfg.UUID,
				JobConfig:  jobConfig,
				JobName:    factory.jobConfig.Name,
				Metadata:   factory.metadata,
			}
		}
	}
}

func (p *podLatency) handleUpdatePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if pm, exists := p.metrics[string(pod.UID)]; exists && pm.podReady.IsZero() {
		for _, c := range pod.Status.Conditions {
			if c.Status == v1.ConditionTrue {
				switch c.Type {
				case v1.PodScheduled:
					if pm.scheduled.IsZero() {
						pm.scheduled = c.LastTransitionTime.Time.UTC()
						pm.NodeName = pod.Spec.NodeName
					}
				case v1.PodInitialized:
					if pm.initialized.IsZero() {
						pm.initialized = c.LastTransitionTime.Time.UTC()
					}
				case v1.ContainersReady:
					if pm.containersReady.IsZero() {
						pm.containersReady = c.LastTransitionTime.Time.UTC()
					}
				case v1.PodReady:
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

func (p *podLatency) setConfig(cfg types.Measurement) error {
	p.config = cfg
	if err := p.validateConfig(); err != nil {
		return err
	}
	return nil
}

// start starts podLatency measurement
func (p *podLatency) start(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	p.metrics = make(map[string]podMetric)
	log.Infof("Creating Pod latency watcher for %s", factory.jobConfig.Name)
	p.watcher = metrics.NewWatcher(
		factory.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
		"podWatcher",
		"pods",
		v1.NamespaceAll,
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
}

// Stop stops podLatency measurement
func (p *podLatency) stop() (int, error) {
	var rc int
	p.watcher.StopWatcher()
	p.normalizeMetrics()
	p.calcQuantiles()
	if len(p.config.LatencyThresholds) > 0 {
		rc = metrics.CheckThreshold(p.config.LatencyThresholds, p.latencyQuantiles)
	}
	if globalCfg.IndexerConfig.Type != "" {
		log.Infof("Indexing pod latency data for job: %s", factory.jobConfig.Name)
		p.index()
	}
	for _, q := range p.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %s 50th: %v 99th: %v max: %v avg: %v", factory.jobConfig.Name, pq.QuantileName, pq.P50, pq.P99, pq.Max, pq.Avg)
	}
	// Reset latency slices, required in multi-job benchmarks
	p.latencyQuantiles, p.normLatencies = nil, nil
	return rc, nil
}

// index sends metrics to the configured indexer
func (p *podLatency) index() {
	metricMap := map[string][]interface{}{
		podLatencyMeasurement:          p.normLatencies,
		podLatencyQuantilesMeasurement: p.latencyQuantiles,
	}
	if p.config.PodLatencyMetrics == types.Quantiles {
		delete(metricMap, podLatencyMeasurement)
	}
	for metricName, data := range metricMap {
		indexingOpts := indexers.IndexingOpts{
			MetricName: fmt.Sprintf("%s-%s", metricName, factory.jobConfig.Name),
		}
		log.Debugf("Indexing [%d] documents", len(data))
		resp, err := (*factory.indexer).Index(data, indexingOpts)
		if err != nil {
			log.Error(err.Error())
		} else {
			log.Info(resp)
		}
	}
}

func (p *podLatency) normalizeMetrics() {
	for _, m := range p.metrics {
		// If a pod does not reach the Running state (this timestamp isn't set), we skip that pod
		if m.podReady.IsZero() {
			continue
		}
		// latencyTime should be always larger than zero, however, in some cases, it might be a
		// negative value due to the precision of timestamp can only get to the level of second,
		// the microsecond and nanosecond have been discarded purposely in kubelet, this is
		// because apiserver does not support RFC339NANO.
		m.ContainersReadyLatency = int(m.containersReady.Sub(m.Timestamp).Milliseconds())
		if m.ContainersReadyLatency < 0 {
			m.ContainersReadyLatency = 0
		}
		m.SchedulingLatency = int(m.scheduled.Sub(m.Timestamp).Milliseconds())
		if m.SchedulingLatency < 0 {
			m.SchedulingLatency = 0
		}
		m.InitializedLatency = int(m.initialized.Sub(m.Timestamp).Milliseconds())
		if m.InitializedLatency < 0 {
			m.InitializedLatency = 0
		}
		m.PodReadyLatency = int(m.podReady.Sub(m.Timestamp).Milliseconds())
		if m.PodReadyLatency < 0 {
			m.PodReadyLatency = 0
		}
		p.normLatencies = append(p.normLatencies, m)
	}
}

func (p *podLatency) calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[v1.PodConditionType][]int{}
	jc := factory.jobConfig
	jc.Objects = nil
	for _, normLatency := range p.normLatencies {
		quantileMap[v1.PodScheduled] = append(quantileMap[v1.PodScheduled], normLatency.(podMetric).SchedulingLatency)
		quantileMap[v1.ContainersReady] = append(quantileMap[v1.ContainersReady], normLatency.(podMetric).ContainersReadyLatency)
		quantileMap[v1.PodInitialized] = append(quantileMap[v1.PodInitialized], normLatency.(podMetric).InitializedLatency)
		quantileMap[v1.PodReady] = append(quantileMap[v1.PodReady], normLatency.(podMetric).PodReadyLatency)
	}
	for quantileName, v := range quantileMap {
		podQ := metrics.LatencyQuantiles{
			QuantileName: string(quantileName),
			UUID:         globalCfg.UUID,
			Timestamp:    time.Now().UTC(),
			JobName:      factory.jobConfig.Name,
			JobConfig:    *jc,
			MetricName:   podLatencyQuantilesMeasurement,
			Metadata:     factory.metadata,
		}
		sort.Ints(v)
		length := len(v)
		if length > 1 {
			for _, quantile := range quantiles {
				qValue := v[int(math.Ceil(float64(length)*quantile))-1]
				podQ.SetQuantile(quantile, qValue)
			}
			podQ.Max = v[length-1]
		}
		sum := 0
		for _, n := range v {
			sum += n
		}
		podQ.Avg = int(math.Round(float64(sum) / float64(length)))
		p.latencyQuantiles = append(p.latencyQuantiles, podQ)
	}
}

func (p *podLatency) validateConfig() error {
	var metricFound bool
	var latencyMetrics = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range p.config.LatencyThresholds {
		if th.ConditionType == string(v1.ContainersReady) || th.ConditionType == string(v1.PodInitialized) || th.ConditionType == string(v1.PodReady) || th.ConditionType == string(v1.PodScheduled) {
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
