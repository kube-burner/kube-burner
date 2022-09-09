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

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/metrics"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
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
	PodReadyLatency        int    `json:"podReadyLatency"`
	MetricName             string `json:"metricName"`
	JobName                string `json:"jobName"`
	UUID                   string `json:"uuid"`
	Namespace              string `json:"namespace"`
	Name                   string `json:"podName"`
	NodeName               string `json:"nodeName"`
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
	pod := obj.(*v1.Pod)
	if _, exists := p.metrics[string(pod.UID)]; !exists {
		if strings.Contains(pod.Namespace, factory.jobConfig.Namespace) {
			p.metrics[string(pod.UID)] = podMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  pod.Namespace,
				Name:       pod.Name,
				MetricName: podLatencyMeasurement,
				UUID:       factory.uuid,
				JobName:    factory.jobConfig.Name,
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
						pm.scheduled = time.Now().UTC()
						pm.NodeName = pod.Spec.NodeName
					}
				case v1.PodInitialized:
					if pm.initialized.IsZero() {
						pm.initialized = time.Now().UTC()
					}
				case v1.ContainersReady:
					if pm.containersReady.IsZero() {
						pm.containersReady = time.Now().UTC()
					}
				case v1.PodReady:
					log.Debugf("Pod %s is ready", pod.Name)
					pm.podReady = time.Now().UTC()
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
	if kubeburnerCfg.WriteToFile {
		if err := metrics.WriteToFile(p.normLatencies, p.latencyQuantiles, "podLatency", factory.jobConfig.Name, kubeburnerCfg.MetricsDirectory); err != nil {
			log.Errorf("Error writing measurement podLatency: %s", err)
		}
	}
	if kubeburnerCfg.IndexerConfig.Enabled {
		p.index()
	}
	for _, q := range p.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %s 50th: %v 99th: %v max: %v avg: %v", factory.jobConfig.Name, pq.QuantileName, pq.P50, pq.P99, pq.Max, pq.Avg)
	}
	// Reset latency slices, required in multi-job benchmarks
	p.latencyQuantiles = nil
	p.normLatencies = nil
	return rc, nil
}

// index sends metrics to the configured indexer
func (p *podLatency) index() {
	(*factory.indexer).Index(p.config.ESIndex, p.normLatencies)
	(*factory.indexer).Index(p.config.ESIndex, p.latencyQuantiles)
}

func (p *podLatency) normalizeMetrics() {
	for _, m := range p.metrics {
		// If a does not reach the Running state (this timestamp wasn't set), we skip that pod
		if m.podReady.IsZero() {
			continue
		}
		m.SchedulingLatency = int(m.scheduled.Sub(m.Timestamp).Milliseconds())
		m.ContainersReadyLatency = int(m.containersReady.Sub(m.Timestamp).Milliseconds())
		m.InitializedLatency = int(m.initialized.Sub(m.Timestamp).Milliseconds())
		m.PodReadyLatency = int(m.podReady.Sub(m.Timestamp).Milliseconds())
		p.normLatencies = append(p.normLatencies, m)
	}
}

func (p *podLatency) calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[v1.PodConditionType][]int{}
	for _, normLatency := range p.normLatencies {
		quantileMap[v1.PodScheduled] = append(quantileMap[v1.PodScheduled], normLatency.(podMetric).SchedulingLatency)
		quantileMap[v1.ContainersReady] = append(quantileMap[v1.ContainersReady], normLatency.(podMetric).ContainersReadyLatency)
		quantileMap[v1.PodInitialized] = append(quantileMap[v1.PodInitialized], normLatency.(podMetric).InitializedLatency)
		quantileMap[v1.PodReady] = append(quantileMap[v1.PodReady], normLatency.(podMetric).PodReadyLatency)
	}
	for quantileName, v := range quantileMap {
		podQ := metrics.LatencyQuantiles{
			QuantileName: string(quantileName),
			UUID:         factory.uuid,
			Timestamp:    time.Now().UTC(),
			JobName:      factory.jobConfig.Name,
			MetricName:   podLatencyQuantilesMeasurement,
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
	var latencyMetrics []string = []string{"P99", "P95", "P50", "Avg", "Max"}
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
