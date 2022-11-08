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
		Short: "kube-burner OpenShift wrapper",
		Long:  `This subcommand is meant to be used against OpenShift clusters and serve as a shortcut to trigger well-known workloads`,
	}
	var wh workloads.WorkloadHelper
	esServer := ocpCmd.PersistentFlags().String("es-server", "", "Elastic Search endpoint")
	esIndex := ocpCmd.PersistentFlags().String("es-index", "", "Elastic Search index")
	qps := ocpCmd.PersistentFlags().Int("qps", 20, "kube-burner QPS")
	burst := ocpCmd.PersistentFlags().Int("burst", 20, "Kube-burner Burst")
	uuid := ocpCmd.PersistentFlags().String("uuid", uid.NewV4().String(), "Benchmark UUID")
	indexing := ocpCmd.PersistentFlags().Bool("indexing", true, "Elastic Search indexing")
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
		wh = workloads.NewWorkloadHelper(envVars)
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
