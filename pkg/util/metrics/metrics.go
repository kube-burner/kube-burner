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
func ProcessMetricsScraperConfig(scraperConfig ScraperConfig) Scraper {
	var err error
	var indexerList []indexers.Indexer
	var metricsEndpoints []metricEndpoint
	var prometheusClients []*prometheus.Prometheus
	var alertM *alerting.AlertManager
	var alertMs []*alerting.AlertManager
	if scraperConfig.MetricsEndpoint != "" && scraperConfig.URL != "" {
		log.Fatal("Please use either of --metrics-endpoint or --prometheus-url flags to fetch metrics or alerts")
	}
	metadata := make(map[string]interface{})
	for pos, indexer := range scraperConfig.ConfigSpec.Indexers {
		if indexer.Type != "" {
			log.Infof("📁 Creating indexer: %s", indexer.Type)
			idx, err := indexers.NewIndexer(indexer.IndexerConfig)
			if err != nil {
				log.Fatalf("Error creating indexer %d: %v", pos, err.Error())
			}
			indexerList = append(indexerList, *idx)
		}
	}
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
	// Set up prometheusClients when Prometheus URl or MetricsEndpoint are set
	if scraperConfig.URL != "" || scraperConfig.MetricsEndpoint != "" {
		if scraperConfig.MetricsEndpoint != "" {
			DecodeMetricsEndpoint(scraperConfig.MetricsEndpoint, &metricsEndpoints)
		} else {
			for _, metricsProfile := range scraperConfig.MetricsProfiles {
				metricsEndpoints = append(metricsEndpoints, metricEndpoint{
					Endpoint:    scraperConfig.URL,
					Token:       scraperConfig.Token,
					Profile:     metricsProfile,
					Username:    scraperConfig.Username,
					Password:    scraperConfig.Password,
					EmbedConfig: scraperConfig.EmbedConfig,
				})
			}
			for _, alertProfile := range scraperConfig.AlertProfiles {
				metricsEndpoints = append(metricsEndpoints, metricEndpoint{
					Endpoint:     scraperConfig.URL,
					Token:        scraperConfig.Token,
					AlertProfile: alertProfile,
					Username:     scraperConfig.Username,
					Password:     scraperConfig.Password,
					EmbedConfig:  scraperConfig.EmbedConfig,
				})
			}
		}
		for _, metricsEndpoint := range metricsEndpoints {
			auth := prometheus.Auth{
				Username:      metricsEndpoint.Username,
				Password:      metricsEndpoint.Password,
				Token:         metricsEndpoint.Token,
				SkipTLSVerify: scraperConfig.SkipTLSVerify,
			}
			p, err := prometheus.NewPrometheusClient(scraperConfig.ConfigSpec, metricsEndpoint.Endpoint, auth, scraperConfig.PrometheusStep, metadata, metricsEndpoint.EmbedConfig, indexerList...)
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
				if alertM, err = alerting.NewAlertManager(metricsEndpoint.AlertProfile, scraperConfig.ConfigSpec.GlobalConfig.UUID, p, metricsEndpoint.EmbedConfig, indexerList...); err != nil {
					log.Fatalf("Error creating alert manager: %s", err)
				}
				alertMs = append(alertMs, alertM)
			}
			prometheusClients = append(prometheusClients, p)
		}
	}
	return Scraper{
		PrometheusClients: prometheusClients,
		AlertMs:           alertMs,
		IndexerList:       indexerList,
		Metadata:          metadata,
	}
}
