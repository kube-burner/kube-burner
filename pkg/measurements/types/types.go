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

	v1 "k8s.io/api/core/v1"
)

// Measurement holds the measurement configuration
type Measurement struct {
	// Name is the name the measurement
	Name string `yaml:"name"`
	// ESIndex contains the ElasticSearch index used to
	// index the metrics
	ESIndex string `yaml:"esIndex"`
	// LatencyThresholds config
	LatencyThresholds []LatencyThreshold `yaml:"thresholds"`
	// PPRofTargets targets config
	PProfTargets []PProftarget `yaml:"pprofTargets"`
	// PPRofInterval pprof collect interval
	PProfInterval time.Duration `yaml:"pprofInterval"`
	// PProfDirectory output directory
	PProfDirectory string `yaml:"pprofDirectory"`
}

// LatencyThreshold holds the thresholds configuration
type LatencyThreshold struct {
	// ConditionType
	ConditionType v1.PodConditionType `yaml:"conditionType"`
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
	// Cert Client certificate file
	Cert string `yaml:"cert"`
	// Key Private key file
	Key string `yaml:"key"`
}
