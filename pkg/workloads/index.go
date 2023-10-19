// Copyright 2022 The Kube-burner Authors.
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

package workloads

import (
	"encoding/json"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// NewIndex orchestrates indexing for ocp wrapper
func NewIndex(metricsEndpoint *string, metadata *BenchmarkMetadata, ocpMetaAgent *ocpmetadata.Metadata) *cobra.Command {
	var metricsProfile, jobName string
	var start, end int64
	var userMetadata, metricsDirectory string
	var prometheusStep time.Duration
	var uuid string
	cmd := &cobra.Command{
		Use:          "index",
		Short:        "Runs index sub-command",
		Long:         "If no other indexer is specified, local indexer is used by default",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			uuid, _ = cmd.InheritedFlags().GetString("uuid")
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", uuid)
		},
		Run: func(cmd *cobra.Command, args []string) {
			clusterMetadata, err := ocpMetaAgent.GetClusterMetadata()
			if err != nil {
				log.Fatal("Error obtaining clusterMetadata: ", err.Error())
			}
			ocpMetadata := make(map[string]interface{})
			jsonData, err := json.Marshal(clusterMetadata)
			if err != nil {
				log.Fatal("Error getting clusterMetadata json: ", err.Error())
			}
			if err := json.Unmarshal(jsonData, &ocpMetadata); err != nil {
				log.Fatal("Errar unmarshalling clusterMetadata: ", err.Error())
			}
			esServer, _ := cmd.Flags().GetString("es-server")
			esIndex, _ := cmd.Flags().GetString("es-index")
			configSpec.GlobalConfig.UUID = uuid
			if esServer != "" && esIndex != "" {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:    indexers.ElasticIndexer,
					Servers: []string{esServer},
					Index:   esIndex,
				}
			} else {
				if metricsDirectory == "collected-metrics" {
					metricsDirectory = metricsDirectory + "-" + uuid
				}
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:             indexers.LocalIndexer,
					MetricsDirectory: metricsDirectory,
				}
			}
			prometheusURL, prometheusToken, err := ocpMetaAgent.GetPrometheus()
			if err != nil {
				log.Fatal("Error obtaining prometheus information from cluster: ", err.Error())
			}
			scraperOutput := metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
				ConfigSpec:      configSpec,
				PrometheusStep:  prometheusStep,
				MetricsEndpoint: *metricsEndpoint,
				MetricsProfile:  metricsProfile,
				SkipTLSVerify:   true,
				URL:             prometheusURL,
				Token:           prometheusToken,
				StartTime:       start,
				EndTime:         end,
				JobName:         jobName,
				ActionIndex:     true,
				UserMetaData:    userMetadata,
				OcpMetaData:     ocpMetadata,
			})
			IndexMetadata(scraperOutput.Indexer, *metadata)
		},
	}
	cmd.Flags().StringVarP(&metricsProfile, "metrics-profile", "m", "metrics.yml", "Metrics profile file")
	cmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "collected-metrics", "Directory to dump the metrics files in, when using default local indexing")
	cmd.Flags().DurationVar(&prometheusStep, "step", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64Var(&start, "start", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64Var(&end, "end", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVar(&jobName, "job-name", "kube-burner-ocp-indexing", "Indexing job name")
	cmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	cmd.Flags().SortFlags = false
	return cmd
}
