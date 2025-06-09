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

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"golang.org/x/exp/maps"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	supportedLatencyMetricsMap = map[string]struct{}{
		"P99": {},
		"P95": {},
		"P50": {},
		"Avg": {},
		"Max": {},
	}
)

type BaseMeasurementFactory struct {
	Config   types.Measurement
	Uuid     string
	Runid    string
	Metadata map[string]any
}

func NewBaseMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) BaseMeasurementFactory {
	return BaseMeasurementFactory{
		Config:   measurement,
		Uuid:     configSpec.GlobalConfig.UUID,
		Runid:    configSpec.GlobalConfig.RUNID,
		Metadata: metadata,
	}
}

func (bmf BaseMeasurementFactory) NewBaseLatency(
	jobConfig *config.Job,
	clientSet kubernetes.Interface,
	restConfig *rest.Config,
	measurementName,
	quantilesMeasurementName string,
	embedCfg *fileutils.EmbedConfiguration,
) BaseMeasurement {
	return BaseMeasurement{
		Config:                   bmf.Config,
		Uuid:                     bmf.Uuid,
		Runid:                    bmf.Runid,
		JobConfig:                jobConfig,
		ClientSet:                clientSet,
		RestConfig:               restConfig,
		Metadata:                 bmf.Metadata,
		MeasurementName:          measurementName,
		QuantilesMeasurementName: quantilesMeasurementName,
		EmbedCfg:                 embedCfg,
	}
}

func verifyMeasurementConfig(config types.Measurement, supportedConditions map[string]struct{}) error {
	for _, th := range config.LatencyThresholds {
		if _, supported := supportedConditions[th.ConditionType]; !supported {
			return fmt.Errorf("unsupported condition type in measurement: %s", th.ConditionType)
		}
		if _, supportedLatency := supportedLatencyMetricsMap[th.Metric]; !supportedLatency {
			return fmt.Errorf("unsupported metric %s in measurement, supported are: %s", th.Metric, strings.Join(maps.Keys(supportedLatencyMetricsMap), ", "))
		}
	}
	return nil
}
