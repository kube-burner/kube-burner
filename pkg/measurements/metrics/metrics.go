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

package metrics

import (
	"fmt"
	"reflect"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/montanaflynn/stats"
	log "github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// LatencyQuantiles holds the latency measurement quantiles
type LatencyQuantiles struct {
	QuantileName string      `json:"quantileName"`
	UUID         string      `json:"uuid"`
	P99          int         `json:"P99"`
	P95          int         `json:"P95"`
	P50          int         `json:"P50"`
	Max          int         `json:"max"`
	Avg          int         `json:"avg"`
	Timestamp    time.Time   `json:"timestamp"`
	MetricName   string      `json:"metricName"`
	JobConfig    config.Job  `json:"jobConfig"`
	Metadata     interface{} `json:"metadata,omitempty"`
}

// CheckThreshold checks latency thresholds
// returns a concatenated list of error strings with a new line between each string
func CheckThreshold(thresholds []types.LatencyThreshold, quantiles []interface{}) error {
	errs := []error{}
	log.Info("Evaluating latency thresholds")
	for _, phase := range thresholds {
		for _, pq := range quantiles {
			if phase.ConditionType == pq.(LatencyQuantiles).QuantileName {
				// Required to acccess the attribute by name
				r := reflect.ValueOf(pq.(LatencyQuantiles))
				v := r.FieldByName(phase.Metric).Int()
				if v > phase.Threshold.Milliseconds() {
					latency := float32(v) / 1000
					err := fmt.Errorf("podLatency: %s %s latency (%.2fs) higher than configured threshold: %v", phase.Metric, phase.ConditionType, latency, phase.Threshold)
					errs = append(errs, err)
				}
			}
		}
	}
	return utilerrors.NewAggregate(errs)
}

func NewLatencySummary(input []float64, name string) LatencyQuantiles {
	latencyQuantiles := LatencyQuantiles{
		QuantileName: name,
		Timestamp:    time.Now().UTC(),
	}
	val, _ := stats.Percentile(input, 50)
	latencyQuantiles.P50 = int(val)
	val, _ = stats.Percentile(input, 95)
	latencyQuantiles.P95 = int(val)
	val, _ = stats.Percentile(input, 99)
	latencyQuantiles.P99 = int(val)
	val, _ = stats.Max(input)
	latencyQuantiles.Max = int(val)
	val, _ = stats.Mean(input)
	latencyQuantiles.Avg = int(val)
	return latencyQuantiles
}
