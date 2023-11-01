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
	"os"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// NewIndex orchestrates indexing for ocp wrapper
func NewIndex(metricsEndpoint *string, ocpMetaAgent *ocpmetadata.Metadata) *cobra.Command {
	var metricsProfile, jobName string
	var start, end int64
	var userMetadata, metricsDirectory string
	var prometheusStep time.Duration
	var uuid string
	var rc int
	var prometheusURL, prometheusToken string
	var tarballName string
	cmd := &cobra.Command{
		Use:          "index",
		Short:        "Runs index sub-command",
		Long:         "If no other indexer is specified, local indexer is used by default",
		SilenceUsage: true,
		PostRun: func(cmd *cobra.Command, args []string) {
			log.Info("👋 Exiting kube-burner ", uuid)
			os.Exit(rc)
		},
		Run: func(cmd *cobra.Command, args []string) {
			uuid, _ = cmd.Flags().GetString("uuid")
			clusterMetadata, err := ocpMetaAgent.GetClusterMetadata()
			if err != nil {
				log.Fatal("Error obtaining clusterMetadata: ", err.Error())
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
			// When metricsEndpoint is specified, don't fetch any prometheus token
			if *metricsEndpoint == "" {
				prometheusURL, prometheusToken, err = ocpMetaAgent.GetPrometheus()
				if err != nil {
					log.Fatal("Error obtaining prometheus information from cluster: ", err.Error())
				}
			}
			metadata := map[string]interface{}{
				"platform":        clusterMetadata.Platform,
				"ocpVersion":      clusterMetadata.OCPVersion,
				"ocpMajorVersion": clusterMetadata.OCPMajorVersion,
				"k8sVersion":      clusterMetadata.K8SVersion,
				"totalNodes":      clusterMetadata.TotalNodes,
				"sdnType":         clusterMetadata.SDNType,
			}
			metricsScraper := metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
				ConfigSpec:      configSpec,
				PrometheusStep:  prometheusStep,
				MetricsEndpoint: *metricsEndpoint,
				MetricsProfile:  metricsProfile,
				SkipTLSVerify:   true,
				URL:             prometheusURL,
				Token:           prometheusToken,
				UserMetaData:    userMetadata,
				RawMetadata:     metadata,
			})
			docsToIndex := make(map[string][]interface{})
			for _, prometheusClients := range metricsScraper.PrometheusClients {
				prometheusJob := prometheus.Job{
					Start: time.Unix(start, 0),
					End:   time.Unix(end, 0),
					JobConfig: config.Job{
						Name: jobName,
					},
				}
				prometheusClients.JobList = append(prometheusClients.JobList, prometheusJob)
				if prometheusClients.ScrapeJobsMetrics(docsToIndex) != nil {
					rc = 1
				}
			}
			log.Infof("Indexing metrics with UUID %s", uuid)
			metrics.IndexDatapoints(docsToIndex, configSpec.GlobalConfig.IndexerConfig.Type, metricsScraper.Indexer)
			if configSpec.GlobalConfig.IndexerConfig.Type == indexers.LocalIndexer && tarballName != "" {
				if err := metrics.CreateTarball(configSpec.GlobalConfig.IndexerConfig, tarballName); err != nil {
					log.Fatal(err)
				}
			}
		},
	}
	cmd.Flags().StringVar(&metricsProfile, "metrics-profile", "metrics.yml", "Metrics profile file")
	cmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "collected-metrics", "Directory to dump the metrics files in, when using default local indexing")
	cmd.Flags().DurationVar(&prometheusStep, "step", 30*time.Second, "Prometheus step size")
	cmd.Flags().Int64Var(&start, "start", time.Now().Unix()-3600, "Epoch start time")
	cmd.Flags().Int64Var(&end, "end", time.Now().Unix(), "Epoch end time")
	cmd.Flags().StringVar(&jobName, "job-name", "kube-burner-ocp-indexing", "Indexing job name")
	cmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	cmd.Flags().StringVar(&tarballName, "tarball-name", "", "Dump collected metrics into a tarball with the given name, requires local indexing")
	cmd.Flags().SortFlags = false
	return cmd
}
