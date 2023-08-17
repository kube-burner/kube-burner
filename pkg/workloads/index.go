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
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// NewIndex orchestrates indexing for ocp wrapper
func NewIndex(wh *WorkloadHelper) *cobra.Command {
	var metricsProfile, jobName string
	var start, end int64
	var userMetadata, metricsDirectory string
	var prometheusStep time.Duration
	var err error
	cmd := &cobra.Command{
		Use:          "index",
		Short:        "Runs index sub-command",
		Long:         "If no other indexer is specified, local indexer is used by default",
		SilenceUsage: true,
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", wh.Metadata.UUID)
		},
		Run: func(cmd *cobra.Command, args []string) {
			ocpMetadata := map[string]interface{}{
				"platform":        wh.Metadata.Platform,
				"ocpVersion":      wh.Metadata.OCPVersion,
				"ocpMajorVersion": wh.Metadata.OCPMajorVersion,
				"k8sVersion":      wh.Metadata.K8SVersion,
				"totalNodes":      wh.Metadata.TotalNodes,
				"sdnType":         wh.Metadata.SDNType,
			}
			if wh.envVars["ES_SERVER"] != "" && wh.envVars["ES_INDEX"] != "" {
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:    indexers.ElasticIndexer,
					Servers: []string{wh.envVars["ES_SERVER"]},
					Index:   wh.envVars["ES_INDEX"],
				}
			} else {
				if metricsDirectory == "collected-metrics" {
					metricsDirectory = metricsDirectory + "-" + wh.Metadata.UUID
				}
				configSpec.GlobalConfig.IndexerConfig = indexers.IndexerConfig{
					Type:             indexers.LocalIndexer,
					MetricsDirectory: metricsDirectory,
				}
			}
			wh.prometheusURL, wh.prometheusToken, err = wh.ocpMetaAgent.GetPrometheus()
			if err != nil {
				log.Fatal("Error obtaining prometheus information from cluster: ", err.Error())
			}
			scraperOutput := metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
				ConfigSpec:      configSpec,
				PrometheusStep:  prometheusStep,
				MetricsEndpoint: wh.metricsEndpoint,
				MetricsProfile:  metricsProfile,
				SkipTLSVerify:   true,
				URL:             wh.prometheusURL,
				Token:           wh.prometheusToken,
				StartTime:       start,
				EndTime:         end,
				JobName:         jobName + "-" + wh.Metadata.UUID,
				ActionIndex:     true,
				UserMetaData:    userMetadata,
				OcpMetaData:     ocpMetadata,
			})
			wh.indexMetadata(scraperOutput.Indexer)
		},
	}
	cmd.Flags().StringVar(&metricsProfile, "metrics-profile", "metrics.yml", "Metrics profile file")
	cmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "collected-metrics", "Directory to dump the metrics files in, when using default local indexing")
	cmd.Flags().DurationVar(&prometheusStep, "step", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64Var(&start, "start", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64Var(&end, "end", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVar(&jobName, "job-name", "kube-burner-ocp-indexing", "Indexing job name")
	cmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	cmd.Flags().SortFlags = false
	return cmd
}
