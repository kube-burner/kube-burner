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

	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"github.com/vishnuchalla/go-commons/indexers"
)

// Processes common config and executes according to the caller
func ProcessMetricsScraperConfig(metricsScraperConfig ScraperConfig) Scraper {
	var err error
	configSpec := metricsScraperConfig.ConfigSpec
	var indexer *indexers.Indexer
	var metricsEndpoints []prometheus.MetricEndpoint
	var prometheusClients []*prometheus.Prometheus
	var alertMs []*alerting.AlertManager
	var alertM *alerting.AlertManager
	userMetadataContent := make(map[string]interface{})
	if configSpec.GlobalConfig.IndexerConfig.Enabled {
		indexerConfig := configSpec.GlobalConfig.IndexerConfig
		log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
		indexer, err = indexers.NewIndexer(indexerConfig)
		if err != nil {
			log.Fatalf("%v indexer: %v", indexerConfig.Type, err.Error())
		}
	}
	if metricsScraperConfig.UserMetaData != "" {
		userMetadataContent, err = util.ReadUserMetadata(metricsScraperConfig.UserMetaData)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
	}
	// When a metric profile or a alert profile is passed we set up metricsEndpoints
	if metricsScraperConfig.MetricsEndpoint != "" || metricsScraperConfig.MetricsProfile != "" || metricsScraperConfig.AlertProfile != "" {
		updateParamIfEmpty(&metricsScraperConfig.MetricsEndpoint, configSpec.GlobalConfig.MetricsEndpoint)
		updateParamIfEmpty(&metricsScraperConfig.URL, configSpec.GlobalConfig.PrometheusURL)
		validateMetricsEndpoint(metricsScraperConfig.MetricsEndpoint, metricsScraperConfig.URL)
		if metricsScraperConfig.MetricsEndpoint != "" {
			DecodeMetricsEndpoint(metricsScraperConfig.MetricsEndpoint, &metricsEndpoints)
		} else {
			updateParamIfEmpty(&metricsScraperConfig.Token, configSpec.GlobalConfig.BearerToken)
			metricsEndpoints = append(metricsEndpoints, prometheus.MetricEndpoint{
				Endpoint: metricsScraperConfig.URL,
				Token:    metricsScraperConfig.Token,
			})
		}
	}
	for _, metricsEndpoint := range metricsEndpoints {
		if metricsEndpoint.Profile != "" {
			configSpec.GlobalConfig.MetricsProfile = metricsEndpoint.Profile
		} else if metricsScraperConfig.MetricsProfile != "" {
			configSpec.GlobalConfig.MetricsProfile = metricsScraperConfig.MetricsProfile
			metricsEndpoint.Profile = metricsScraperConfig.MetricsProfile
		}
		metadataContent := map[string]interface{}{}
		if metricsScraperConfig.ActionIndex {
			metadataContent = userMetadataContent
		}
		p, err := prometheus.NewPrometheusClient(configSpec, metricsEndpoint.Endpoint, metricsEndpoint.Token, metricsScraperConfig.Username, metricsScraperConfig.Password, metricsScraperConfig.UUID, metricsScraperConfig.SkipTLSVerify, metricsScraperConfig.PrometheusStep, metadataContent)
		if err != nil {
			log.Fatal(err)
		}
		if metricsScraperConfig.ActionIndex {
			if metricsEndpoint.Start == 0 || metricsEndpoint.End == 0 || metricsEndpoint.Start == metricsEndpoint.End {
				metricsEndpoint.Start = metricsScraperConfig.StartTime
				metricsEndpoint.End = metricsScraperConfig.EndTime
			}

			p.JobList = []prometheus.Job{{
				Start: time.Unix(metricsEndpoint.Start, 0),
				End:   time.Unix(metricsEndpoint.End, 0),
				Name:  metricsScraperConfig.JobName,
			},
			}
			ScrapeMetrics(p, indexer)
			if configSpec.GlobalConfig.IndexerConfig.Type == indexers.LocalIndexer {
				HandleTarball(configSpec)
			}
		} else {
			updateParamIfEmpty(&metricsEndpoint.AlertProfile, metricsScraperConfig.AlertProfile)
			updateParamIfEmpty(&metricsEndpoint.AlertProfile, configSpec.GlobalConfig.AlertProfile)
			if metricsEndpoint.AlertProfile != "" {
				if alertM, err = alerting.NewAlertManager(metricsEndpoint.AlertProfile, metricsScraperConfig.UUID, configSpec.GlobalConfig.IndexerConfig.Index, indexer, p); err != nil {
					log.Fatalf("Error creating alert manager: %s", err)
				}
			}
			prometheusClients = append(prometheusClients, p)
			alertMs = append(alertMs, alertM)
			alertM = nil
		}
	}
	return Scraper{
		PrometheusClients:   prometheusClients,
		AlertMs:             alertMs,
		Indexer:             indexer,
		ConfigSpec:          configSpec,
		UserMetadataContent: userMetadataContent,
	}
}
