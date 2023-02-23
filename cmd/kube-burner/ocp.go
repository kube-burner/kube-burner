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
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/cloud-bulldozer/kube-burner/pkg/workloads"
	uid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
)

//go:embed ocp-config/*
var OCPConfig embed.FS

func openShiftCmd() *cobra.Command {
	ocpCmd := &cobra.Command{
		Use:   "ocp",
		Short: "OpenShift wrapper",
		Long:  `This subcommand is meant to be used against OpenShift clusters and serve as a shortcut to trigger well-known workloads`,
	}
	var wh workloads.WorkloadHelper
	esServer := ocpCmd.PersistentFlags().String("es-server", "", "Elastic Search endpoint")
	esIndex := ocpCmd.PersistentFlags().String("es-index", "", "Elastic Search index")
	alerting := ocpCmd.PersistentFlags().Bool("alerting", true, "Enable alerting")
	uuid := ocpCmd.PersistentFlags().String("uuid", uid.NewV4().String(), "Benchmark UUID")
	timeout := ocpCmd.PersistentFlags().Duration("timeout", 3*time.Hour, "Benchmark timeout")
	qps := ocpCmd.PersistentFlags().Int("qps", 20, "QPS")
	burst := ocpCmd.PersistentFlags().Int("burst", 20, "Burst")
	gc := ocpCmd.PersistentFlags().Bool("gc", true, "Garbage collect created namespaces")
	userMetadata := ocpCmd.PersistentFlags().String("user-metadata", "", "User provided metadata file, in YAML format")
	extract := ocpCmd.PersistentFlags().Bool("extract", false, "Extract workload in the current directory")
	ocpCmd.MarkFlagsRequiredTogether("es-server", "es-index")
	ocpCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		rootCmd.PersistentPreRun(cmd, args)
		indexing := *esServer != ""
		envVars := map[string]string{
			"ES_SERVER": strings.TrimSuffix(*esServer, "/"),
			"ES_INDEX":  *esIndex,
			"QPS":       fmt.Sprintf("%d", *qps),
			"BURST":     fmt.Sprintf("%d", *burst),
			"INDEXING":  fmt.Sprintf("%v", indexing),
			"GC":        fmt.Sprintf("%v", *gc),
		}
		discoveryAgent := discovery.NewDiscoveryAgent()
		wh = workloads.NewWorkloadHelper(envVars, *alerting, OCPConfig, discoveryAgent, indexing, *timeout, *userMetadata)
		wh.Metadata.UUID = *uuid
		if *extract {
			if err := wh.ExtractWorkload(cmd.Name(), workloads.MetricsProfileMap[cmd.Name()]); err != nil {
				log.Fatal(err)
			}
			os.Exit(0)
		}
		if *esServer != "" {
			err := wh.GatherMetadata()
			if err != nil {
				log.Fatal(err.Error())
			}
		}
		wh.SetKubeBurnerFlags()
	}
	ocpCmd.AddCommand(
		workloads.NewClusterDensity(&wh, "cluster-density"),
		workloads.NewClusterDensity(&wh, "cluster-density-v2"),
		workloads.NewNodeDensity(&wh),
		workloads.NewNodeDensityHeavy(&wh),
		workloads.NewNodeDensityCNI(&wh),
	)
	return ocpCmd
}
