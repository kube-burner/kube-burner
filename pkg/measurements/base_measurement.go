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
	"fmt"
	"strings"
	"sync"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/v2/pkg/watchers"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type BaseMeasurement struct {
	Config                   types.Measurement
	EmbedCfg                 *fileutils.EmbedConfiguration
	Uuid                     string
	Runid                    string
	JobConfig                *config.Job
	ClientSet                kubernetes.Interface
	RestConfig               *rest.Config
	Metadata                 map[string]any
	watchers                 []*watchers.Watcher
	Metrics                  sync.Map
	MeasurementName          string
	LatencyQuantiles         []any
	QuantilesMeasurementName string
	NormLatencies            []any
	LabelSelector            string
}

type MeasurementWatcher struct {
	dynamicClient dynamic.Interface
	name          string
	resource      schema.GroupVersionResource
	labelSelector string
	handlers      *cache.ResourceEventHandlerFuncs
	transform     cache.TransformFunc
}

func (bm *BaseMeasurement) startMeasurement(measurementWatchers []MeasurementWatcher) {
	// Reset latency slices, required in multi-job benchmarks
	bm.LatencyQuantiles, bm.NormLatencies = nil, nil
	bm.Metrics = sync.Map{}

	bm.watchers = make([]*watchers.Watcher, len(measurementWatchers))
	for i, measurementWatcher := range measurementWatchers {
		var selectors []string
		if bm.LabelSelector != "" {
			selectors = []string{bm.LabelSelector}
		}
		if measurementWatcher.labelSelector != "" {
			selectors = append(selectors, measurementWatcher.labelSelector)
		}
		log.Infof("Creating %v latency watcher for %s using selector %s", measurementWatcher.resource, bm.JobConfig.Name, selectors)
		bm.watchers[i] = watchers.NewWatcher(
			measurementWatcher.dynamicClient,
			measurementWatcher.name,
			measurementWatcher.resource,
			corev1.NamespaceAll,
			func(options *metav1.ListOptions) {
				if len(selectors) > 0 {
					options.LabelSelector = strings.Join(selectors, ",")
				}
			},
			nil,
		)
		// Apply transform function before starting the informer to reduce memory usage
		if measurementWatcher.transform != nil {
			if err := bm.watchers[i].Informer.SetTransform(measurementWatcher.transform); err != nil {
				log.Warnf("failed to set transform for %s: %v", measurementWatcher.name, err)
			}
		}
		if measurementWatcher.handlers != nil {
			bm.watchers[i].Informer.AddEventHandler(measurementWatcher.handlers)
		}
		if err := bm.watchers[i].StartAndCacheSync(); err != nil {
			log.Errorf("%v Latency measurement error: %s", measurementWatcher.resource, err)
		}
	}
}

func (bm *BaseMeasurement) stopWatchers() {
	for _, watcher := range bm.watchers {
		watcher.StopWatcher()
	}
}

func (bm *BaseMeasurement) StopMeasurement(normalizeMetrics func() float64, getLatency func(any) map[string]float64) error {
	var err error
	bm.stopWatchers()
	errorRate := normalizeMetrics()
	if errorRate > 10.00 {
		log.Error("Latency errors beyond 10%. Hence invalidating the results")
		return fmt.Errorf("something is wrong with system under test. %v latencies error rate was: %.2f", bm.MeasurementName, errorRate)
	}
	bm.calculateQuantiles(getLatency)
	if len(bm.Config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(bm.Config.LatencyThresholds, bm.LatencyQuantiles)
	}
	for _, q := range bm.LatencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %v 99th: %vms max: %vms avg: %vms", bm.JobConfig.Name, pq.QuantileName, pq.P99, pq.Max, pq.Avg)
	}
	if errorRate > 0 {
		log.Infof("%v error rate was: %.2f", bm.MeasurementName, errorRate)
	}
	return err
}

func (bm *BaseMeasurement) GetMetrics() *sync.Map {
	return &bm.Metrics
}

func (bm *BaseMeasurement) Index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]any{
		bm.MeasurementName:          bm.NormLatencies,
		bm.QuantilesMeasurementName: bm.LatencyQuantiles,
	}
	bm.indexLatencyMeasurement(jobName, metricMap, indexerList)
}

// Keep this method to allow reuse when overriding Index
func (bm *BaseMeasurement) indexLatencyMeasurement(jobName string, metricMap map[string][]any, indexerList map[string]indexers.Indexer) {
	indexDocuments := func(indexer indexers.Indexer, metricName string, data []any) {
		log.Infof("Indexing metric %s", metricName)
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
	for metricName, data := range metricMap {
		// Use the configured TimeseriesIndexer or QuantilesIndexer when specified or else use all indexers
		if bm.Config.TimeseriesIndexer != "" && (metricName == podLatencyMeasurement || metricName == svcLatencyMeasurement || metricName == nodeLatencyMeasurement || metricName == pvcLatencyMeasurement) {
			indexer := indexerList[bm.Config.TimeseriesIndexer]
			indexDocuments(indexer, metricName, data)
		} else if bm.Config.QuantilesIndexer != "" && (metricName == podLatencyQuantilesMeasurement || metricName == svcLatencyQuantilesMeasurement || metricName == nodeLatencyQuantilesMeasurement || metricName == pvcLatencyQuantilesMeasurement) {
			indexer := indexerList[bm.Config.QuantilesIndexer]
			indexDocuments(indexer, metricName, data)
		} else {
			for _, indexer := range indexerList {
				indexDocuments(indexer, metricName, data)
			}
		}
	}

}

// Common function to calculate quantiles for both node and pod latencies
// Receives a function to get the latencies for each condition
func (bm *BaseMeasurement) calculateQuantiles(getLatency func(any) map[string]float64) {
	quantileMap := map[string][]float64{}
	for _, normLatency := range bm.NormLatencies {
		for condition, latency := range getLatency(normLatency) {
			quantileMap[condition] = append(quantileMap[condition], latency)
		}
	}
	calcSummary := func(name string, inputLatencies []float64) (metrics.LatencyQuantiles, error) {
		latencySummary, err := metrics.NewLatencySummary(inputLatencies, name)
		if err != nil {
			return latencySummary, err
		}
		latencySummary.UUID = bm.Uuid
		latencySummary.Metadata = bm.Metadata
		latencySummary.MetricName = bm.QuantilesMeasurementName
		latencySummary.JobName = bm.JobConfig.Name
		return latencySummary, nil
	}

	bm.LatencyQuantiles = make([]any, 0, len(quantileMap))
	for condition, latencies := range quantileMap {
		summary, err := calcSummary(condition, latencies)
		if err != nil {
			log.Warnf("Failed to calculate latency summary for %s: %v", condition, err)
			continue
		}
		bm.LatencyQuantiles = append(bm.LatencyQuantiles, summary)
	}
}
