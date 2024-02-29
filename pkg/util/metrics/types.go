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
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/alerting"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
)

// ScraperConfig holds data related to scraper and target indexer
type ScraperConfig struct {
	ConfigSpec      config.Spec
	Password        string
	PrometheusStep  time.Duration
	MetricsEndpoint string
	MetricsProfiles []string
	AlertProfiles   []string
	SkipTLSVerify   bool
	URL             string
	Token           string
	Username        string
	UserMetaData    string
	RawMetadata     map[string]interface{}
	EmbedConfig     bool
}

// ScraperResponse holds parsed data related to scraper and target indexer
type Scraper struct {
	PrometheusClients []*prometheus.Prometheus
	AlertMs           []*alerting.AlertManager
	IndexerList       []indexers.Indexer
	Metadata          map[string]interface{}
}

// metricEndpoint describes prometheus endpoint to scrape
type metricEndpoint struct {
	Endpoint     string `yaml:"endpoint"`
	Token        string `yaml:"token"`
	Profile      string `yaml:"profile"`
	AlertProfile string `yaml:"alertProfile"`
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	EmbedConfig  bool   `yaml:"embedConfig"`
}
