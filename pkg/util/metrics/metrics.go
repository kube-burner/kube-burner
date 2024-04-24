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
	"fmt"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/alerting"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
)

// Processes common config and executes according to the caller
func ProcessMetricsScraperConfig(scraperConfig ScraperConfig) Scraper {
	if len(scraperConfig.ConfigSpec.MetricsEndpoints) == 0 && scraperConfig.MetricsEndpoint == "" {
		return Scraper{}
	}
	var err error
	var indexer *indexers.Indexer
	var indexerAlias string
	indexerList := make(map[string]indexers.Indexer)
	var prometheusClients []*prometheus.Prometheus
	var alertM *alerting.AlertManager
	var alertMs []*alerting.AlertManager
	metadata := make(map[string]interface{})
	if scraperConfig.UserMetaData != "" {
		metadata, err = util.ReadUserMetadata(scraperConfig.UserMetaData)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
	}
	// Combine rawMetadata with user's provided metadata
	for k, v := range scraperConfig.RawMetadata {
		metadata[k] = v
	}
	// MetricsEndpoint has preference over the configuration file
	if scraperConfig.MetricsEndpoint != "" {
		scraperConfig.ConfigSpec.MetricsEndpoints = DecodeMetricsEndpoint(scraperConfig.MetricsEndpoint)
	}
	for pos, metricsEndpoint := range scraperConfig.ConfigSpec.MetricsEndpoints {
		indexer = nil
		if metricsEndpoint.Type != "" {
			if metricsEndpoint.Alias == "" {
				indexerAlias = fmt.Sprintf("indexer-%d", pos)
			} else {
				indexerAlias = metricsEndpoint.Alias
			}
			log.Infof("📁 Creating %s indexer: %s", metricsEndpoint.Type, indexerAlias)
			indexer, err = indexers.NewIndexer(metricsEndpoint.IndexerConfig)
			if err != nil {
				log.Fatalf("Error creating indexer %d: %v", pos, err.Error())
			}
			indexerList[indexerAlias] = *indexer
		}
		if (len(metricsEndpoint.Metrics) > 0 || len(metricsEndpoint.Alerts) > 0) && metricsEndpoint.Endpoint != "" {
			auth := prometheus.Auth{
				Username:      metricsEndpoint.Username,
				Password:      metricsEndpoint.Password,
				Token:         metricsEndpoint.Token,
				SkipTLSVerify: metricsEndpoint.SkipTLSVerify,
			}
			p, err := prometheus.NewPrometheusClient(*scraperConfig.ConfigSpec, metricsEndpoint.Endpoint, auth, metricsEndpoint.Step, metadata, scraperConfig.EmbedConfig, indexer)
			if err != nil {
				log.Fatal(err)
			}
			prometheusClients = append(prometheusClients, p)
			for _, metricProfile := range metricsEndpoint.Metrics {
				if indexer == nil {
					log.Fatalf("Metrics profile is configured for endpoint #%d but no indexer was defined", pos)
				}
				if err := p.ReadProfile(metricProfile); err != nil {
					log.Fatal(err.Error())
				}
			}
			for _, alertProfile := range metricsEndpoint.Alerts {
				if alertM, err = alerting.NewAlertManager(alertProfile, scraperConfig.ConfigSpec.GlobalConfig.UUID, p, scraperConfig.EmbedConfig, indexer); err != nil {
					log.Fatalf("Error creating alert manager: %s", err)
				}
				alertMs = append(alertMs, alertM)
			}
		}
	}
	return Scraper{
		PrometheusClients: prometheusClients,
		AlertMs:           alertMs,
		IndexerList:       indexerList,
		Metadata:          metadata,
	}
}
