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
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
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

// WriteToFile writes both the latency and quantile results to a file
func WriteToFile(lat []interface{}, quant []interface{}, metricName string, jobName string, directory string) error {
	filesMetrics := map[string]interface{}{
		fmt.Sprintf("%s-%s.json", jobName, metricName):         lat,
		fmt.Sprintf("%s-%s-summary.json", jobName, metricName): quant,
	}
	for filename, data := range filesMetrics {
		if directory != "" {
			err := os.MkdirAll(directory, 0744)
			if err != nil {
				return fmt.Errorf("error creating metrics directory: %s", err)
			}
			filename = path.Join(directory, filename)
		}
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("error creating latency metrics file %s: %s", filename, err)
		}
		defer f.Close()
		jsonEnc := json.NewEncoder(f)
		jsonEnc.SetIndent("", "  ")
		log.Infof("Writing latency metrics in %s", filename)
		if err := jsonEnc.Encode(data); err != nil {
			return fmt.Errorf("JSON encoding error: %s", err)
		}
	}
	return nil
}

func CheckThreshold(thresholds []types.LatencyThreshold, quantiles []interface{}) int {
	var rc int
	log.Info("Evaluating latency thresholds")
	for _, phase := range thresholds {
		for _, pq := range quantiles {
			if phase.ConditionType == pq.(LatencyQuantiles).QuantileName {
				// Required to acccess the attribute by name
				r := reflect.ValueOf(pq.(LatencyQuantiles))
				v := r.FieldByName(phase.Metric).Int()
				latency := float32(v) / 1000
				msg := fmt.Sprintf("❗ %s %s latency (%.2fs) higher than configured threshold: %v", phase.Metric, phase.ConditionType, latency, phase.Threshold)
				if v > phase.Threshold.Milliseconds() {
					log.Error(msg)
					rc = 1
				}
				continue
			}
		}
	}
	return rc
}
