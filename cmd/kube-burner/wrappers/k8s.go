// Copyright 2023 The Kube-burner Authors.
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

package wrappers

import (
	"os"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/workloads"
	uid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func K8SCmd() *cobra.Command {
	k8sCmd := &cobra.Command{
		Use:   "k8s",
		Short: "Vanilla k8s wrapper",
		Long:  `This subcommand is meant to be used against vanilla k8s clusters and serve as a shortcut to trigger well-known workloads`,
	}
	var workloadConfig workloads.Config
	var wh workloads.WorkloadHelper
	k8sCmd.PersistentFlags().StringVar(&workloadConfig.EsServer, "es-server", "", "Elastic Search endpoint")
	k8sCmd.PersistentFlags().StringVar(&workloadConfig.EsIndex, "es-index", "", "Elastic Search index")
	localIndexing := k8sCmd.PersistentFlags().Bool("local-indexing", false, "Enable local indexing")
	k8sCmd.PersistentFlags().StringVar(&workloadConfig.MetricsEndpoint, "metrics-endpoint", "", "YAML file with a list of metric endpoints")
	k8sCmd.PersistentFlags().BoolVar(&workloadConfig.Alerting, "alerting", true, "Enable alerting")
	k8sCmd.PersistentFlags().StringVar(&workloadConfig.UUID, "uuid", uid.NewV4().String(), "Benchmark UUID")
	k8sCmd.PersistentFlags().DurationVar(&workloadConfig.Timeout, "timeout", 4*time.Hour, "Benchmark timeout")
	k8sCmd.PersistentFlags().IntVar(&workloadConfig.QPS, "qps", 20, "QPS")
	k8sCmd.PersistentFlags().IntVar(&workloadConfig.Burst, "burst", 20, "Burst")
	k8sCmd.PersistentFlags().BoolVar(&workloadConfig.Gc, "gc", true, "Garbage collect created namespaces")
	k8sCmd.PersistentFlags().BoolVar(&workloadConfig.GcMetrics, "gc-metrics", false, "Collect metrics during garbage collection")
	userMetadata := k8sCmd.PersistentFlags().String("user-metadata", "", "User provided metadata file, in YAML format")
	extract := k8sCmd.PersistentFlags().Bool("extract", false, "Extract workload in the current directory")
	k8sCmd.PersistentFlags().StringVar(&workloadConfig.ProfileType, "profile-type", "both", "Metrics profile to use, supported options are: regular, reporting or both")
	k8sCmd.PersistentFlags().BoolVar(&workloadConfig.Reporting, "reporting", false, "Enable benchmark report indexing")
	k8sCmd.MarkFlagsRequiredTogether("es-server", "es-index")
	k8sCmd.MarkFlagsMutuallyExclusive("es-server", "local-indexing")
	k8sCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		cmd.Root().PersistentPreRun(cmd, args)
		if workloadConfig.EsServer != "" || *localIndexing {
			if workloadConfig.EsServer != "" {
				workloadConfig.Indexer = indexers.ElasticIndexer
			} else {
				workloadConfig.Indexer = indexers.LocalIndexer
			}
		}
		wh = workloads.NewWorkloadHelper(workloadConfig, wrappersConfig, workloads.K8S)
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
	}
	k8sCmd.AddCommand(
		workloads.NewCrdScale(&wh),
		workloads.NewNetworkPolicy(&wh, "networkpolicy-multitenant"),
		workloads.NewNetworkPolicy(&wh, "networkpolicy-matchlabels"),
		workloads.NewNetworkPolicy(&wh, "networkpolicy-matchexpressions"),
		workloads.NewNodeDensity(&wh),
		workloads.NewNodeDensityHeavy(&wh),
		workloads.NewNodeDensityCNI(&wh),
		workloads.NewPVCDensity(&wh),
	)
	return k8sCmd
}
