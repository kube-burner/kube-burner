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
)

// Processes common config and executes according to the caller
func ProcessMetricsScraperConfig(metricsScraperRequest MetricsScraperRequest) MetricsScraperResponse {
	var indexer *indexers.Indexer
	var metricsEndpoints []prometheus.MetricEndpoint
	var prometheusClients []*prometheus.Prometheus
	var alertMs []*alerting.AlertManager
	var alertM *alerting.AlertManager
	configSpec, err := config.Parse(metricsScraperRequest.ConfigFile, false)
	if err != nil {
		log.Fatal(err.Error())
	}
	if configSpec.GlobalConfig.IndexerConfig.Enabled {
		indexer, err = indexers.NewIndexer(configSpec)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
	updateParamIfEmpty(&metricsScraperRequest.MetricsEndpoint, configSpec.GlobalConfig.MetricsEndpoint)
	updateParamIfEmpty(&metricsScraperRequest.URL, configSpec.GlobalConfig.PrometheusURL)
	validateMetricsEndpoint(metricsScraperRequest.MetricsEndpoint, metricsScraperRequest.URL)

	if metricsScraperRequest.MetricsEndpoint != "" {
		DecodeMetricsEndpoint(metricsScraperRequest.MetricsEndpoint, &metricsEndpoints)
	} else {
		updateParamIfEmpty(&metricsScraperRequest.Token, configSpec.GlobalConfig.BearerToken)
		metricsEndpoints = append(metricsEndpoints, prometheus.MetricEndpoint{
			Endpoint: metricsScraperRequest.URL,
			Token:    metricsScraperRequest.Token,
		})
	}

	for _, eachEntry := range metricsEndpoints {

		if eachEntry.Profile != "" {
			configSpec.GlobalConfig.MetricsProfile = eachEntry.Profile
		} else if metricsScraperRequest.MetricsProfile != "" {
			configSpec.GlobalConfig.MetricsProfile = metricsScraperRequest.MetricsProfile
			eachEntry.Profile = metricsScraperRequest.MetricsProfile
		}
		// Updating the prometheus endpoint actually being used in spec.
		configSpec.GlobalConfig.PrometheusURL = eachEntry.Endpoint

		p, err := prometheus.NewPrometheusClient(configSpec, eachEntry.Endpoint, eachEntry.Token, metricsScraperRequest.Username, metricsScraperRequest.Password, metricsScraperRequest.UUID, metricsScraperRequest.SkipTLSVerify, metricsScraperRequest.PrometheusStep)
		if err != nil {
			log.Fatal(err)
		}
		if metricsScraperRequest.ActionIndex {
			if eachEntry.Start == eachEntry.End {
				eachEntry.Start = metricsScraperRequest.StartTime
				eachEntry.End = metricsScraperRequest.EndTime
			}

			p.JobList = []prometheus.Job{{
				Start: time.Unix(eachEntry.Start, 0),
				End:   time.Unix(eachEntry.End, 0),
				Name:  metricsScraperRequest.JobName,
			},
			}
			ScrapeMetrics(p, indexer)
			HandleTarball(configSpec)
		} else {
			updateParamIfEmpty(&eachEntry.AlertProfile, metricsScraperRequest.AlertProfile)
			updateParamIfEmpty(&eachEntry.AlertProfile, configSpec.GlobalConfig.AlertProfile)
			if eachEntry.AlertProfile != "" {
				if alertM, err = alerting.NewAlertManager(eachEntry.AlertProfile, metricsScraperRequest.UUID, configSpec.GlobalConfig.IndexerConfig.DefaultIndex, indexer, p); err != nil {
					log.Fatalf("Error creating alert manager: %s", err)
				}
			}
			prometheusClients = append(prometheusClients, p)
			alertMs = append(alertMs, alertM)
			alertM = nil
		}
	}
	return MetricsScraperResponse{
		PrometheusClients: prometheusClients,
		AlertMs:           alertMs,
		Indexer:           indexer,
		ConfigSpec:        configSpec,
	}
}
