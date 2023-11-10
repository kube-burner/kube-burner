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

package main

import (
	"embed"
	_ "embed"
	"os"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/workloads"
	uid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

//go:embed ocp-config/*
var ocpConfig embed.FS

func openShiftCmd() *cobra.Command {
	ocpCmd := &cobra.Command{
		Use:   "ocp",
		Short: "OpenShift wrapper",
		Long:  `This subcommand is meant to be used against OpenShift clusters and serve as a shortcut to trigger well-known workloads`,
	}
	var workloadConfig workloads.Config
	var wh workloads.WorkloadHelper
	ocpCmd.PersistentFlags().StringVar(&workloadConfig.EsServer, "es-server", "", "Elastic Search endpoint")
	ocpCmd.PersistentFlags().StringVar(&workloadConfig.Esindex, "es-index", "", "Elastic Search index")
	localIndexing := ocpCmd.PersistentFlags().Bool("local-indexing", false, "Enable local indexing")
	ocpCmd.PersistentFlags().StringVar(&workloadConfig.MetricsEndpoint, "metrics-endpoint", "", "YAML file with a list of metric endpoints")
	ocpCmd.PersistentFlags().BoolVar(&workloadConfig.Alerting, "alerting", true, "Enable alerting")
	ocpCmd.PersistentFlags().StringVar(&workloadConfig.UUID, "uuid", uid.NewV4().String(), "Benchmark UUID")
	ocpCmd.PersistentFlags().DurationVar(&workloadConfig.Timeout, "timeout", 4*time.Hour, "Benchmark timeout")
	ocpCmd.PersistentFlags().IntVar(&workloadConfig.QPS, "qps", 20, "QPS")
	ocpCmd.PersistentFlags().IntVar(&workloadConfig.Burst, "burst", 20, "Burst")
	ocpCmd.PersistentFlags().BoolVar(&workloadConfig.Gc, "gc", true, "Garbage collect created namespaces")
	ocpCmd.PersistentFlags().BoolVar(&workloadConfig.GcMetrics, "gc-metrics", false, "Collect metrics during garbage collection")
	userMetadata := ocpCmd.PersistentFlags().String("user-metadata", "", "User provided metadata file, in YAML format")
	extract := ocpCmd.PersistentFlags().Bool("extract", false, "Extract workload in the current directory")
	ocpCmd.PersistentFlags().StringVar(&workloadConfig.ProfileType, "profile-type", "both", "Metrics profile to use, supported options are: regular, reporting or both")
	ocpCmd.PersistentFlags().BoolVar(&workloadConfig.Reporting, "reporting", false, "Enable benchmark report indexing")
	ocpCmd.MarkFlagsRequiredTogether("es-server", "es-index")
	ocpCmd.MarkFlagsMutuallyExclusive("es-server", "local-indexing")
	ocpCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		rootCmd.PersistentPreRun(cmd, args)
		if workloadConfig.EsServer != "" || *localIndexing {
			if workloadConfig.EsServer != "" {
				workloadConfig.Indexer = indexers.ElasticIndexer
			} else {
				workloadConfig.Indexer = indexers.LocalIndexer
			}
		}
		wh = workloads.NewWorkloadHelper(workloadConfig, ocpConfig)
		if *extract {
			if err := wh.ExtractWorkload(cmd.Name(), workloads.MetricsProfileMap[cmd.Name()]); err != nil {
				log.Fatal(err)
			}
			os.Exit(0)
		}
		err := wh.GatherMetadata(*userMetadata)
		if err != nil {
			log.Fatal(err.Error())
		}
		wh.SetKubeBurnerFlags()
	}
	ocpCmd.AddCommand(
		workloads.NewClusterDensity(&wh, "cluster-density"),
		workloads.NewClusterDensity(&wh, "cluster-density-v2"),
		workloads.NewClusterDensity(&wh, "cluster-density-ms"),
		workloads.NewCrdScale(&wh),
		workloads.NewNetworkPolicy(&wh, "networkpolicy-multitenant"),
		workloads.NewNetworkPolicy(&wh, "networkpolicy-matchlabels"),
		workloads.NewNetworkPolicy(&wh, "networkpolicy-matchexpressions"),
		workloads.NewNodeDensity(&wh),
		workloads.NewNodeDensityHeavy(&wh),
		workloads.NewNodeDensityCNI(&wh),
		workloads.NewIndex(&wh.MetricsEndpoint, &wh.OcpMetaAgent),
		workloads.NewPVCDensity(&wh),
	)
	return ocpCmd
}
