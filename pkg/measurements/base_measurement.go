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
	"sync"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/pkg/watchers"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	metrics                  sync.Map
	MeasurementName          string
	latencyQuantiles         []any
	QuantilesMeasurementName string
	normLatencies            []any
}

type MeasurementWatcher struct {
	restClient    *rest.RESTClient
	name          string
	resource      string
	labelSelector string
	handlers      *cache.ResourceEventHandlerFuncs
}

func (bm *BaseMeasurement) startMeasurement(measurementWatchers []MeasurementWatcher) {
	// Reset latency slices, required in multi-job benchmarks
	bm.latencyQuantiles, bm.normLatencies = nil, nil
	bm.metrics = sync.Map{}

	bm.watchers = make([]*watchers.Watcher, len(measurementWatchers))
	for i, measurementWatcher := range measurementWatchers {
		log.Infof("Creating %v latency watcher for %s", measurementWatcher.resource, bm.JobConfig.Name)
		bm.watchers[i] = watchers.NewWatcher(
			measurementWatcher.restClient,
			measurementWatcher.name,
			measurementWatcher.resource,
			corev1.NamespaceAll,
			func(options *metav1.ListOptions) {
				if measurementWatcher.labelSelector != "" {
					options.LabelSelector = measurementWatcher.labelSelector
				}
			},
			nil,
		)
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
	defer bm.stopWatchers()
	errorRate := normalizeMetrics()
	if errorRate > 10.00 {
		log.Error("Latency errors beyond 10%. Hence invalidating the results")
		return fmt.Errorf("something is wrong with system under test. %v latencies error rate was: %.2f", bm.MeasurementName, errorRate)
	}
	bm.calculateQuantiles(getLatency)
	if len(bm.Config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(bm.Config.LatencyThresholds, bm.latencyQuantiles)
	}
	for _, q := range bm.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %v 99th: %v max: %v avg: %v", bm.JobConfig.Name, pq.QuantileName, pq.P99, pq.Max, pq.Avg)
	}
	if errorRate > 0 {
		log.Infof("%v error rate was: %.2f", bm.MeasurementName, errorRate)
	}
	return err
}

func (bm *BaseMeasurement) GetMetrics() *sync.Map {
	return &bm.metrics
}

func (bm *BaseMeasurement) Index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]any{
		bm.MeasurementName:          bm.normLatencies,
		bm.QuantilesMeasurementName: bm.latencyQuantiles,
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
	for _, normLatency := range bm.normLatencies {
		for condition, latency := range getLatency(normLatency) {
			quantileMap[condition] = append(quantileMap[condition], latency)
		}
	}
	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = bm.Uuid
		latencySummary.Metadata = bm.Metadata
		latencySummary.MetricName = bm.QuantilesMeasurementName
		latencySummary.JobName = bm.JobConfig.Name
		return latencySummary
	}

	bm.latencyQuantiles = make([]any, 0, len(quantileMap))
	for condition, latencies := range quantileMap {
		bm.latencyQuantiles = append(bm.latencyQuantiles, calcSummary(condition, latencies))
	}
}
