// Copyright 2021 The Kube-burner Authors.
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

package types

import (
	"time"
)

const (
	pprofDirectory string = "pprof"
)

// UnmarshalYAML implements Unmarshaller to customize object defaults
func (m *Measurement) UnmarshalMeasurement(unmarshal func(any) error) error {
	type rawMeasurement Measurement
	measurement := rawMeasurement{
		PProfDirectory: pprofDirectory,
		ServiceTimeout: 5 * time.Second,
	}
	if err := unmarshal(&measurement); err != nil {
		return err
	}
	*m = Measurement(measurement)
	return nil
}

// Measurement holds the measurement configuration
type Measurement struct {
	// Name is the name the measurement
	Name string `yaml:"name"`
	// LatencyThresholds config
	LatencyThresholds []LatencyThreshold `yaml:"thresholds"`
	// PPRofTargets targets config
	PProfTargets []PProftarget `yaml:"pprofTargets"`
	// PPRofInterval pprof collect interval
	PProfInterval time.Duration `yaml:"pprofInterval"`
	// PProfDirectory output directory
	PProfDirectory string `yaml:"pprofDirectory"`
	// NodeAffinity node affinity configuration
	NodeAffinity map[string]string `yaml:"nodeAffinity"`
	// Service latency endpoint timeout
	ServiceTimeout time.Duration `yaml:"svcTimeout"`
	// Defines the indexer for quantile metrics
	QuantilesIndexer string `yaml:"quantilesIndexer"`
	// Defines the indexer for timeseries
	TimeseriesIndexer string `yaml:"timeseriesIndexer"`
}

// LatencyThreshold holds the thresholds configuration
type LatencyThreshold struct {
	// ConditionType
	ConditionType string `yaml:"conditionType"`
	// Metric type
	Metric string `yaml:"metric"`
	// Threshold accepted
	Threshold time.Duration `yaml:"threshold"`
}

// PProftarget pprof targets to collect
type PProftarget struct {
	// Name pprof target name
	Name string `yaml:"name"`
	// Namespace pod namespace
	Namespace string `yaml:"namespace"`
	// LabelSelector get pprof from pods with these labels
	LabelSelector map[string]string `yaml:"labelSelector"`
	// BearerToken bearer token
	BearerToken string `yaml:"bearerToken"`
	// URL target URL
	URL string `yaml:"url"`
	// CertFile Client certificate file
	CertFile string `yaml:"certFile"`
	// KeyFile Private key file
	KeyFile string `yaml:"keyFile"`
	// Cert Client certificate content
	Cert string `yaml:"cert"`
	// Key Private key content
	Key string `yaml:"key"`
}

const (
	SvcLatencyNs          = "kube-burner-service-latency"
	SvcLatencyCheckerName = "svc-checker"
)

const (
	PprofNamespace   = "pprof-collector"
	PprofDaemonSet   = "pprof-collector"
	PprofSA          = "pprof-collector"
	PprofRole        = "pprof-collector"
	PprofRoleBinding = "pprof-collector"
)
