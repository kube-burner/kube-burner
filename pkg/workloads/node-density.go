package workloads

import (
	"fmt"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"

	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density workload
func NewNodeDensity(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, rc int
	var podReadyThreshold time.Duration
	var containerImage string
	var alertM *alerting.AlertManager
	cmd := &cobra.Command{
		Use:          "node-density",
		Short:        "Runs node-density workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			workerNodeCount, err := discovery.GetWorkerNodeCount()
			if err != nil {
				log.Fatal("Error obtaining worker node count:", err)
			}
			totalPods := workerNodeCount * podsPerNode
			podCount, err := discovery.GetCurrentPodCount()
			if err != nil {
				log.Fatal(err)
			}
			jobIterations := totalPods - podCount
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(jobIterations))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("CONTAINER_IMAGE", containerImage)
		},
		Run: func(cmd *cobra.Command, args []string) {
			configSpec, err := config.Parse("node-density.yml", true)
			if err != nil {
				log.Fatal(err)
			}
			configSpec.GlobalConfig.MetricsProfile = metricsProfile
			p, err := prometheus.NewPrometheusClient(configSpec, wh.prometheusURL, wh.prometheusToken, "", "", wh.Metadata.UUID, true, 30*time.Second)
			if err != nil {
				log.Fatal(err)
			}
			if wh.alerting {
				alertM, err = alerting.NewAlertManager(alertsProfile, p)
				if err != nil {
					log.Fatal(err)
				}
			}
			rc, err = burner.Run(configSpec, wh.Metadata.UUID, p, alertM)
			if err != nil {
				log.Fatal(err)
			}
			wh.Metadata.Passed = rc == 0
			wh.IndexMetadata()
			os.Exit(rc)
		},
	}
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 5*time.Second, "Pod ready timeout threshold")
	cmd.Flags().StringVar(&containerImage, "container-image", "gcr.io/google_containers/pause:3.1", "Container image")
	return cmd
}
