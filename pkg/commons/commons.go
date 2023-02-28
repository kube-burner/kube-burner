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
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
)

// Processes common config and executes according to the caller
func ProcessMetricsScraperConfig(metricsScraperConfig MetricsScraperConfig) MetricsScraper {
	var indexer *indexers.Indexer
	var metricsEndpoints []prometheus.MetricEndpoint
	var prometheusClients []*prometheus.Prometheus
	var alertMs []*alerting.AlertManager
	var alertM *alerting.AlertManager
	userMetadataContent := make(map[string]interface{})
	configSpec, err := config.Parse(metricsScraperConfig.ConfigFile, false)
	if err != nil {
		log.Fatal(err.Error())
	}
	if configSpec.GlobalConfig.IndexerConfig.Enabled {
		indexer, err = indexers.NewIndexer(configSpec)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
	if metricsScraperConfig.UserMetaData != "" {
		userMetadataContent, err = util.ReadUserMetadata(metricsScraperConfig.UserMetaData)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
	}

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
			HandleTarball(configSpec)
		} else {
			updateParamIfEmpty(&metricsEndpoint.AlertProfile, metricsScraperConfig.AlertProfile)
			updateParamIfEmpty(&metricsEndpoint.AlertProfile, configSpec.GlobalConfig.AlertProfile)
			if metricsEndpoint.AlertProfile != "" {
				if alertM, err = alerting.NewAlertManager(metricsEndpoint.AlertProfile, metricsScraperConfig.UUID, configSpec.GlobalConfig.IndexerConfig.DefaultIndex, indexer, p); err != nil {
					log.Fatalf("Error creating alert manager: %s", err)
				}
			}
			prometheusClients = append(prometheusClients, p)
			alertMs = append(alertMs, alertM)
			alertM = nil
		}
	}
	return MetricsScraper{
		PrometheusClients:   prometheusClients,
		AlertMs:             alertMs,
		Indexer:             indexer,
		ConfigSpec:          configSpec,
		UserMetadataContent: userMetadataContent,
	}
}
