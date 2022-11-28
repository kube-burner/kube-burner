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
	"fmt"
	"log"
	"strings"

	_ "embed"

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
	qps := ocpCmd.PersistentFlags().Int("qps", 20, "QPS")
	burst := ocpCmd.PersistentFlags().Int("burst", 20, "Burst")
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
		}
		wh = workloads.NewWorkloadHelper(envVars, *alerting, OCPConfig)
		wh.Metadata.UUID = *uuid
		if *esServer != "" {
			err := wh.GatherMetadata()
			if err != nil {
				log.Fatal(err.Error())
			}
		}
		wh.SetKubeBurnerFlags()
	}
	ocpCmd.PersistentPostRun = func(cmd *cobra.Command, args []string) {
		if *esServer != "" {
			wh.IndexMetadata()
		}
	}
	ocpCmd.AddCommand(
		workloads.NewClusterDensity(&wh),
		workloads.NewNodeDensity(&wh),
		workloads.NewNodeDensityHeavy(&wh),
	)
	return ocpCmd
}
