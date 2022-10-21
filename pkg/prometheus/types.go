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
	"net/http"
	"time"

	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// Prometheus describes the prometheus connection
type Prometheus struct {
	api           apiv1.API
	MetricProfile metricProfile
	Pause         time.Duration
	Step          time.Duration
	uuid          string
}

// This object implements RoundTripper
type authTransport struct {
	Transport http.RoundTripper
	token     string
	username  string
	password  string
}

// metricProfile describes what metrics kube-burner collects
type metricProfile []struct {
	Query      string `yaml:"query"`
	MetricName string `yaml:"metricName"`
	IndexName  string `yaml:"indexName"`
	Instant    bool   `yaml:"instant"`
}

type metric struct {
	Timestamp  time.Time         `json:"timestamp"`
	Labels     map[string]string `json:"labels"`
	Value      float64           `json:"value"`
	UUID       string            `json:"uuid"`
	Query      string            `json:"query"`
	MetricName string            `json:"metricName,omitempty"`
	JobName    string            `json:"jobName,omitempty"`
}
