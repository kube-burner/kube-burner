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

package commons

import (
	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
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
		log.Fatal("Please use either of --metrics-endpoint or --prometheus-url flags to index the metrics")
	}
}

// Decodes metrics endpoint yaml file
func DecodeMetricsEndpoint(metricsEndpoint string, metricsEndpoints *[]prometheus.MetricEndpoint) {
	f, err := util.ReadConfig(metricsEndpoint)
	if err != nil {
		log.Fatalf("Error reading metrics endpoint %s: %s", metricsEndpoint, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err := yamlDec.Decode(&metricsEndpoints); err != nil {
		log.Fatalf("Error decoding metrics endpoint %s: %s", metricsEndpoint, err)
	}
}

// Scrapes prometheus metrics
func ScrapeMetrics(p *prometheus.Prometheus, indexer *indexers.Indexer) {
	log.Infof("Scraping for the prometheus entry with params - {Endpoint:%v, Profile:%v, Start:%v, End:%v}\n",
		p.ConfigSpec.GlobalConfig.PrometheusURL,
		p.ConfigSpec.GlobalConfig.MetricsProfile,
		p.JobList[0].Start,
		p.JobList[len(p.JobList)-1].End)
	log.Infof("Indexing metrics with UUID %s", p.UUID)
	if err := p.ScrapeJobsMetrics(indexer); err != nil {
		log.Error(err)
	}
}

// Handles tarball use case
func HandleTarball(configSpec config.Spec) {
	var err error
	if configSpec.GlobalConfig.WriteToFile && configSpec.GlobalConfig.CreateTarball {
		err = prometheus.CreateTarball(configSpec.GlobalConfig.MetricsDirectory)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}
