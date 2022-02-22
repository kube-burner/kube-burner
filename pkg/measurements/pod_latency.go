// Copyright 2019 The Kube-burner Authors.
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
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/watcher"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

type podLatency struct {
	config           types.Measurement
	watchers         map[string]*watcher.Watcher
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

func init() {
	MeasurementMap[types.PodLatency] = &podLatency{watchers: make(map[string]*watcher.Watcher)}
}

func (p *podLatency) SetConfig(cfg types.Measurement) error {
	p.config = cfg
	if err := p.validateConfig(); err != nil {
		return err
	}
	return nil
}

// start starts podLatency measurement
func (p *podLatency) Start(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	log.Infof("Creating Pod latency watcher for %s", factory.JobConfig.Name)
	p.createPodWatcherIfNotExist()
}

func (p *podLatency) createPodWatcherIfNotExist() {
	if _, exist := p.watchers[types.PodWatcher]; !exist {
		w := watcher.NewPodWatcher(
			factory.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
			v1.NamespaceAll,
			types.PodLatencyMeasurement,
			factory.UUID,
			factory.JobConfig.Name,
		)
		p.RegisterWatcher(types.PodWatcher, w)
	}
}

func (p *podLatency) RegisterWatcher(name string, w *watcher.Watcher) {
	p.watchers[name] = w
}

func (p *podLatency) GetWatcher(name string) (*watcher.Watcher, bool) {
	w, exists := p.watchers[name]
	return w, exists
}

// Stop stops podLatency measurement
func (p *podLatency) Stop() (int, error) {
	var rc int
	p.watchers[types.PodWatcher].StopWatcher()
	p.normalizeMetrics()
	p.calcQuantiles()
	if len(p.config.LatencyThresholds) > 0 {
		rc = metrics.CheckThreshold(p.config.LatencyThresholds, p.latencyQuantiles)
	}
	if kubeburnerCfg.WriteToFile {
		if err := metrics.WriteToFile(p.normLatencies, p.latencyQuantiles, types.PodWatcher, factory.JobConfig.Name, kubeburnerCfg.MetricsDirectory); err != nil {
			log.Errorf("Error writing measurement %s: %s", types.PodWatcher, err)
		}
	}
	if kubeburnerCfg.IndexerConfig.Enabled {
		p.index()
	}
	p.watchers[types.PodWatcher].CleanMetrics()
	return rc, nil
}

// index sends metrics to the configured indexer
func (p *podLatency) index() {
	(*factory.Indexer).Index(p.config.ESIndex, p.normLatencies)
	(*factory.Indexer).Index(p.config.ESIndex, p.latencyQuantiles)
}

func (p *podLatency) normalizeMetrics() {
	for _, m := range p.watchers[types.PodWatcher].GetMetrics() {
		podM := m.(*metrics.PodMetric)
		// If a does not reach the Running state (this timestamp wasn't set), we skip that pod
		if podM.PodReady == nil {
			continue
		}
		podM.PodScheduledLatency = int(podM.PodScheduled.Sub(podM.Timestamp).Milliseconds())
		podM.PodContainersReadyLatency = int(podM.PodContainersReady.Sub(podM.Timestamp).Milliseconds())
		podM.PodInitializedLatency = int(podM.PodInitialized.Sub(podM.Timestamp).Milliseconds())
		podM.PodReadyLatency = int(podM.PodReady.Sub(podM.Timestamp).Milliseconds())
		p.normLatencies = append(p.normLatencies, m)
	}
}

func (p *podLatency) calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[v1.PodConditionType][]int{}
	for _, normLatency := range p.normLatencies {
		quantileMap[v1.PodScheduled] = append(quantileMap[v1.PodScheduled], normLatency.(*metrics.PodMetric).PodScheduledLatency)
		quantileMap[v1.ContainersReady] = append(quantileMap[v1.ContainersReady], normLatency.(*metrics.PodMetric).PodContainersReadyLatency)
		quantileMap[v1.PodInitialized] = append(quantileMap[v1.PodInitialized], normLatency.(*metrics.PodMetric).PodInitializedLatency)
		quantileMap[v1.PodReady] = append(quantileMap[v1.PodReady], normLatency.(*metrics.PodMetric).PodReadyLatency)
	}
	for quantileName, v := range quantileMap {
		podQ := metrics.LatencyQuantiles{
			QuantileName: string(quantileName),
			UUID:         factory.UUID,
			Timestamp:    time.Now().UTC(),
			JobName:      factory.JobConfig.Name,
			MetricName:   types.PodLatencyQuantilesMeasurement,
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
