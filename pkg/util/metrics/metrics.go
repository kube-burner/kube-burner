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
	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/alerting"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
)

// Processes common config and executes according to the caller
func ProcessMetricsScraperConfig(metricsScraperConfig ScraperConfig) Scraper {
	var err error
	var indexer *indexers.Indexer
	var metricsEndpoints []prometheus.MetricEndpoint
	var prometheusClients []*prometheus.Prometheus
	var alertM *alerting.AlertManager
	var alertMs []*alerting.AlertManager
	metadata := make(map[string]interface{})
	if metricsScraperConfig.ConfigSpec.GlobalConfig.IndexerConfig.Type != "" {
		indexerConfig := metricsScraperConfig.ConfigSpec.GlobalConfig.IndexerConfig
		log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
		indexer, err = indexers.NewIndexer(indexerConfig)
		if err != nil {
			log.Fatalf("%v indexer: %v", indexerConfig.Type, err.Error())
		}
	}
	if metricsScraperConfig.UserMetaData != "" {
		metadata, err = util.ReadUserMetadata(metricsScraperConfig.UserMetaData)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
	}
	// Combine rawMetadata with user's provided metadata
	for k, v := range metricsScraperConfig.RawMetadata {
		metadata[k] = v
	}
	// When a metric profile or a alert profile is passed we set up metricsEndpoints
	if metricsScraperConfig.MetricsEndpoint != "" || metricsScraperConfig.MetricsProfile != "" || metricsScraperConfig.AlertProfile != "" {
		validateMetricsEndpoint(metricsScraperConfig.MetricsEndpoint, metricsScraperConfig.URL)
		if metricsScraperConfig.MetricsEndpoint != "" {
			DecodeMetricsEndpoint(metricsScraperConfig.MetricsEndpoint, &metricsEndpoints)
		} else {
			metricsEndpoints = append(metricsEndpoints, prometheus.MetricEndpoint{
				Endpoint:     metricsScraperConfig.URL,
				Token:        metricsScraperConfig.Token,
				Profile:      metricsScraperConfig.MetricsProfile,
				AlertProfile: metricsScraperConfig.AlertProfile,
			})
		}
	}
	for _, metricsEndpoint := range metricsEndpoints {
		auth := prometheus.Auth{
			Username:      metricsScraperConfig.Username,
			Password:      metricsScraperConfig.Password,
			Token:         metricsEndpoint.Token,
			SkipTLSVerify: metricsScraperConfig.SkipTLSVerify,
		}
		p, err := prometheus.NewPrometheusClient(metricsScraperConfig.ConfigSpec, metricsEndpoint.Endpoint, auth, metricsScraperConfig.PrometheusStep, metadata, indexer, false)
		if err != nil {
			log.Fatal(err)
		}
		if metricsEndpoint.Profile != "" {
			err = p.ReadProfile(metricsEndpoint.Profile)
			if err != nil {
				log.Fatal(err)
			}
		}
		if metricsEndpoint.AlertProfile != "" {
			if alertM, err = alerting.NewAlertManager(metricsEndpoint.AlertProfile, metricsScraperConfig.ConfigSpec.GlobalConfig.UUID, indexer, p, false); err != nil {
				log.Fatalf("Error creating alert manager: %s", err)
			}
			alertMs = append(alertMs, alertM)
		}
		prometheusClients = append(prometheusClients, p)
	}
	return Scraper{
		PrometheusClients: prometheusClients,
		AlertMs:           alertMs,
		Indexer:           indexer,
		Metadata:          metadata,
	}
}
