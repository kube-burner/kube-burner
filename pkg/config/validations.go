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

package config

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
)

func validatePodLatencyCfg(cfg Measurement) error {
	var metricFound bool
	var latencyMetrics []string = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range cfg.LatencyThresholds {
		if th.ConditionType == v1.ContainersReady || th.ConditionType == v1.PodInitialized || th.ConditionType == v1.PodReady || th.ConditionType == v1.PodScheduled {
			for _, lm := range latencyMetrics {
				if th.Metric == lm {
					metricFound = true
					break
				}
			}
			if !metricFound {
				return fmt.Errorf("Unsupported metric %s in podLatency measurement, supported are: %s", th.Metric, strings.Join(latencyMetrics, ", "))
			}
		} else {
			return fmt.Errorf("Unsupported pod condition type in podLatency measurement: %s", th.ConditionType)
		}
	}
	return nil
}

func validatePprofCfg(cfg Measurement) error {
	return nil
}
