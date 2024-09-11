// Copyright 2024 The Kube-burner Authors.
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

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
)

func IndexLatencyMeasurement(config types.Measurement, jobName string, metricMap map[string][]interface{}, indexerList map[string]indexers.Indexer) {
	indexDocuments := func(indexer indexers.Indexer, metricName string, data []interface{}) {
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
		if config.TimeseriesIndexer != "" && (metricName == podLatencyMeasurement || metricName == svcLatencyMeasurement || metricName == nodeLatencyMeasurement) {
			indexer := indexerList[config.TimeseriesIndexer]
			indexDocuments(indexer, metricName, data)
		} else if config.QuantilesIndexer != "" && (metricName == podLatencyQuantilesMeasurement || metricName == svcLatencyQuantilesMeasurement || metricName == nodeLatencyQuantilesMeasurement) {
			indexer := indexerList[config.QuantilesIndexer]
			indexDocuments(indexer, metricName, data)
		} else {
			for _, indexer := range indexerList {
				indexDocuments(indexer, metricName, data)
			}
		}
	}

}

// Common function to calculate quantiles for both node and pod latencies
func calculateQuantiles(normLatencies []interface{}, quantileMap map[string][]float64, getLatency func(interface{}) map[string]float64, metricName string) []interface{} {
	for _, normLatency := range normLatencies {
		for condition, latency := range getLatency(normLatency) {
			quantileMap[condition] = append(quantileMap[condition], latency)
		}
	}

	calcSummary := func(name string, inputLatencies []float64) metrics.LatencyQuantiles {
		latencySummary := metrics.NewLatencySummary(inputLatencies, name)
		latencySummary.UUID = globalCfg.UUID
		latencySummary.Metadata = factory.metadata
		latencySummary.MetricName = metricName
		latencySummary.JobName = factory.jobConfig.Name
		return latencySummary
	}

	var latencyQuantiles []interface{}
	for condition, latencies := range quantileMap {
		latencyQuantiles = append(latencyQuantiles, calcSummary(condition, latencies))
	}

	return latencyQuantiles
}
