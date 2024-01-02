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
	JobName      string      `json:"jobName"`
	JobConfig    config.Job  `json:"jobConfig"`
	Metadata     interface{} `json:"metadata,omitempty"`
}

// SetQuantile adds quantile value
func (plq *LatencyQuantiles) SetQuantile(quantile float64, qValue int) {
	switch quantile {
	case 0.5:
		plq.P50 = qValue
	case 0.95:
		plq.P95 = qValue
	case 0.99:
		plq.P99 = qValue
	}
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
