package workloads

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/spf13/cobra"
)

// NewClusterDensity holds cluster-density workload
func NewClusterDensity(wh *WorkloadHelper) *cobra.Command {
	var iterations, rc int
	cmd := &cobra.Command{
		Use:   "cluster-density <flags>",
		Short: "Runs cluster-density workload",
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(iterations))
		},
		Run: func(cmd *cobra.Command, args []string) {
			configSpec, err := config.Parse("cluster-density.yml", true)
			if err != nil {
				log.Fatal(err)
			}
			configSpec.GlobalConfig.MetricsProfile = metricsProfile
			p, err := prometheus.NewPrometheusClient(configSpec, wh.prometheusURL, wh.prometheusToken, "", "", wh.Metadata.UUID, true, 30*time.Second)
			if err != nil {
				log.Fatal(err)
			}
			alertM, err := alerting.NewAlertManager(alertsProfile, p)
			if err != nil {
				log.Fatal(err)
			}
			rc, err = burner.Run(configSpec, wh.Metadata.UUID, p, alertM)
			if err != nil {
				log.Fatal(err)
			}
			wh.Metadata.Passed = rc != 0
			wh.IndexMetadata()
			os.Exit(rc)
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", 0, "Cluster-density iterations")
	cmd.MarkFlagRequired("iterations")
	return cmd
}
