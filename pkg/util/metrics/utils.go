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

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

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
	renderedME, err := util.RenderTemplate("", cfg, util.EnvToMap(), util.MissingKeyError)
	if err != nil {
		log.Fatalf("Template error in %s: %s", metricsEndpoint, err)
	}
	yamlDec := yaml.NewDecoder(bytes.NewReader(renderedME))
	yamlDec.KnownFields(true)
	if err := yamlDec.Decode(&metricsEndpoints); err != nil {
		log.Fatalf("Error decoding metricsEndpoint %s: %s", metricsEndpoint, err)
	}
}

// Indexes datapoints to a specified indexer.
func IndexDatapoints(docsToIndex map[string][]interface{}, indexerType indexers.IndexerType, indexer *indexers.Indexer) {
	for metricName, docs := range docsToIndex {
		if indexerType != "" {
			log.Infof("Indexing metric %s", metricName)
			log.Debugf("Indexing [%d] documents", len(docs))
			resp, err := (*indexer).Index(docs, indexers.IndexingOpts{MetricName: metricName})
			if err != nil {
				log.Error(err.Error())
			} else {
				log.Info(resp)
			}
		}
	}
}
