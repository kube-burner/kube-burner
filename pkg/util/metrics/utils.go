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
	"bytes"
	"io"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Updates parameter in place with new value if its is empty
func updateParamIfEmpty(oldValue *string, newValue string) {
	if *oldValue == "" {
		*oldValue = newValue
	}
}

// Performs the validity check of metrics endpoint and prometheus url
func validateMetricsEndpoint(metricsEndpoint string, prometheusURL string) {
	if (metricsEndpoint != "" && prometheusURL != "") || (metricsEndpoint == "" && prometheusURL == "") {
		log.Fatal("Please use either of --metrics-endpoint or --prometheus-url flags to fetch metrics or alerts")
	}
}

// Decodes metrics endpoint yaml file
func DecodeMetricsEndpoint(metricsEndpoint string, metricsEndpoints *[]prometheus.MetricEndpoint) {
	f, err := util.ReadConfig(metricsEndpoint)
	if err != nil {
		log.Fatalf("Error reading metricsEndpoint %s: %s", metricsEndpoint, err)
	}
	cfg, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("Error reading configuration file %s: %s", metricsEndpoint, err)
	}
	renderedME, err := util.RenderTemplate(cfg, util.EnvToMap(), util.MissingKeyError)
	if err != nil {
		log.Fatalf("Template error in %s: %s", metricsEndpoint, err)
	}
	yamlDec := yaml.NewDecoder(bytes.NewReader(renderedME))
	yamlDec.KnownFields(true)
	if err := yamlDec.Decode(&metricsEndpoints); err != nil {
		log.Fatalf("Error decoding metricsEndpoint %s: %s", metricsEndpoint, err)
	}
}

// Scrapes prometheus metrics
func ScrapeMetrics(p *prometheus.Prometheus, indexer *indexers.Indexer) {
	log.Infof("Scraping for the prometheus entry with params - Endpoint: %v, Profile: %v, Start: %v, End: %v",
		p.Endpoint,
		p.ConfigSpec.GlobalConfig.MetricsProfile,
		p.JobList[0].Start.Format(time.RFC3339),
		p.JobList[len(p.JobList)-1].End.Format(time.RFC3339))
	log.Infof("Indexing metrics with UUID %s", p.UUID)
	if err := p.ScrapeJobsMetrics(indexer); err != nil {
		log.Error(err)
	}
}
