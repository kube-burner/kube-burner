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

	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/montanaflynn/stats"
	log "github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// LatencyQuantiles holds the latency measurement quantiles
type LatencyQuantiles struct {
	QuantileName string    `json:"quantileName"`
	UUID         string    `json:"uuid"`
	P99          int       `json:"P99"`
	P95          int       `json:"P95"`
	P50          int       `json:"P50"`
	Min          int       `json:"min"`
	Max          int       `json:"max"`
	Avg          int       `json:"avg"`
	Timestamp    time.Time `json:"timestamp"`
	MetricName   string    `json:"metricName"`
	JobName      string    `json:"jobName,omitempty"`
	Metadata     any       `json:"metadata,omitempty"`
}

// CheckThreshold checks latency thresholds
// returns a concatenated list of error strings with a new line between each string
func CheckThreshold(thresholds []types.LatencyThreshold, quantiles []any) error {
	errs := []error{}
	log.Info("Evaluating latency thresholds")
	for _, phase := range thresholds {
		for _, pq := range quantiles {
			if phase.ConditionType == pq.(LatencyQuantiles).QuantileName {
				// Required to access the attribute by name
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

func NewLatencySummary(input []float64, name string) (LatencyQuantiles, error) {
	latencyQuantiles := LatencyQuantiles{
		QuantileName: name,
		Timestamp:    time.Now().UTC(),
	}
	if len(input) == 0 {
		return latencyQuantiles, fmt.Errorf("cannot calculate latency summary: empty input slice for %s", name)
	}
	var err error
	var val float64
	if val, err = stats.Percentile(input, 50); err != nil {
		return latencyQuantiles, fmt.Errorf("failed to calculate P50 for %s: %w", name, err)
	}
	latencyQuantiles.P50 = int(val)
	if val, err = stats.Percentile(input, 95); err != nil {
		return latencyQuantiles, fmt.Errorf("failed to calculate P95 for %s: %w", name, err)
	}
	latencyQuantiles.P95 = int(val)
	if val, err = stats.Percentile(input, 99); err != nil {
		return latencyQuantiles, fmt.Errorf("failed to calculate P99 for %s: %w", name, err)
	}
	latencyQuantiles.P99 = int(val)
	if val, err = stats.Min(input); err != nil {
		return latencyQuantiles, fmt.Errorf("failed to calculate Min for %s: %w", name, err)
	}
	latencyQuantiles.Min = int(val)
	if val, err = stats.Max(input); err != nil {
		return latencyQuantiles, fmt.Errorf("failed to calculate Max for %s: %w", name, err)
	}
	latencyQuantiles.Max = int(val)
	if val, err = stats.Mean(input); err != nil {
		return latencyQuantiles, fmt.Errorf("failed to calculate Mean for %s: %w", name, err)
	}
	latencyQuantiles.Avg = int(val)
	return latencyQuantiles, nil
}
