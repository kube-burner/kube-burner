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

package prometheus

import (
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/go-commons/prometheus"
	"github.com/kube-burner/kube-burner/pkg/config"
)

type Auth struct {
	Username      string
	Password      string
	Token         string
	SkipTLSVerify bool
}

// Prometheus describes the prometheus connection
type Prometheus struct {
	Client        *prometheus.Prometheus
	Endpoint      string
	profileName   string
	MetricProfile []metricDefinition
	Step          time.Duration
	UUID          string
	ConfigSpec    config.Spec
	metadata      map[string]interface{}
	embedConfig   bool
	indexers      []indexers.Indexer
}

type Job struct {
	Start      time.Time
	End        time.Time
	ChurnStart time.Time
	ChurnEnd   time.Time
	JobConfig  config.Job
}

// metricDefinition describes what metrics kube-burner collects
type metricDefinition struct {
	Query      string `yaml:"query"`
	MetricName string `yaml:"metricName"`
	Instant    bool   `yaml:"instant"`
}

type metric struct {
	Timestamp   time.Time         `json:"timestamp"`
	Labels      map[string]string `json:"labels,omitempty"`
	Value       float64           `json:"value"`
	UUID        string            `json:"uuid"`
	Query       string            `json:"query"`
	ChurnMetric bool              `json:"churnMetric,omitempty"`
	MetricName  string            `json:"metricName,omitempty"`
	JobConfig   config.Job        `json:"jobConfig,omitempty"`
	Metadata    interface{}       `json:"metadata,omitempty"`
}
