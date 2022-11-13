package main

import (
	"fmt"
	"log"

	"github.com/cloud-bulldozer/kube-burner/pkg/workloads"
	uid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
)

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
	indexing := ocpCmd.PersistentFlags().Bool("indexing", true, "Enable Elastic Search indexing")
	qps := ocpCmd.PersistentFlags().Int("qps", 20, "QPS")
	burst := ocpCmd.PersistentFlags().Int("burst", 20, "Burst")
	ocpCmd.MarkFlagsRequiredTogether("es-server", "es-index")
	ocpCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		rootCmd.PersistentPreRun(cmd, args)
		envVars := map[string]string{
			"ES_SERVER": *esServer,
			"ES_INDEX":  *esIndex,
			"QPS":       fmt.Sprintf("%d", *qps),
			"BURST":     fmt.Sprintf("%d", *burst),
			"INDEXING":  fmt.Sprintf("%v", *indexing),
		}
		wh = workloads.NewWorkloadHelper(envVars, *alerting)
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
