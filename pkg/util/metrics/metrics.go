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
	// When no metrics endpoint is provided do nothing
	if metricsScraperConfig.MetricsEndpoint == "" && len(metricsScraperConfig.MetricsProfiles) == 0 && len(metricsScraperConfig.AlertProfiles) == 0 {
		return Scraper{}
	}
	var err error
	var indexerList []indexers.Indexer
	var metricsEndpoints []metricEndpoint
	var prometheusClients []*prometheus.Prometheus
	var alertM *alerting.AlertManager
	var alertMs []*alerting.AlertManager
	if (metricsScraperConfig.MetricsEndpoint != "" && metricsScraperConfig.URL != "") || (metricsScraperConfig.MetricsEndpoint == "" && metricsScraperConfig.URL == "") {
		log.Fatal("Please use either of --metrics-endpoint or --prometheus-url flags to fetch metrics or alerts")
	}
	metadata := make(map[string]interface{})
	for pos, indexer := range metricsScraperConfig.ConfigSpec.Indexers {
		log.Infof("📁 Creating indexer: %s", indexer.Type)
		idx, err := indexers.NewIndexer(indexer.IndexerConfig)
		if err != nil {
			log.Fatalf("Error creating indexer %d: %v", pos, err.Error())
		}
		indexerList = append(indexerList, *idx)
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
	if metricsScraperConfig.MetricsEndpoint != "" {
		DecodeMetricsEndpoint(metricsScraperConfig.MetricsEndpoint, &metricsEndpoints)
	} else {
		for _, metricsProfile := range metricsScraperConfig.MetricsProfiles {
			metricsEndpoints = append(metricsEndpoints, metricEndpoint{
				Endpoint:    metricsScraperConfig.URL,
				Token:       metricsScraperConfig.Token,
				Profile:     metricsProfile,
				Username:    metricsScraperConfig.Username,
				Password:    metricsScraperConfig.Password,
				EmbedConfig: metricsScraperConfig.EmbedConfig,
			})
		}
		for _, alertProfile := range metricsScraperConfig.AlertProfiles {
			metricsEndpoints = append(metricsEndpoints, metricEndpoint{
				Endpoint:     metricsScraperConfig.URL,
				Token:        metricsScraperConfig.Token,
				AlertProfile: alertProfile,
				Username:     metricsScraperConfig.Username,
				Password:     metricsScraperConfig.Password,
				EmbedConfig:  metricsScraperConfig.EmbedConfig,
			})
		}
	}
	for _, metricsEndpoint := range metricsEndpoints {
		auth := prometheus.Auth{
			Username:      metricsEndpoint.Username,
			Password:      metricsEndpoint.Password,
			Token:         metricsEndpoint.Token,
			SkipTLSVerify: metricsScraperConfig.SkipTLSVerify,
		}
		p, err := prometheus.NewPrometheusClient(metricsScraperConfig.ConfigSpec, metricsEndpoint.Endpoint, auth, metricsScraperConfig.PrometheusStep, metadata, false, indexerList...)
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
			if alertM, err = alerting.NewAlertManager(metricsEndpoint.AlertProfile, metricsScraperConfig.ConfigSpec.GlobalConfig.UUID, p, false, indexerList...); err != nil {
				log.Fatalf("Error creating alert manager: %s", err)
			}
			alertMs = append(alertMs, alertM)
		}
		prometheusClients = append(prometheusClients, p)
	}
	return Scraper{
		PrometheusClients: prometheusClients,
		AlertMs:           alertMs,
		IndexerList:       indexerList,
		Metadata:          metadata,
	}
}
