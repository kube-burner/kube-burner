package workloads

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density-heavy workload
func NewNodeDensityHeavy(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, workerNodeCount, rc int
	var podReadyThreshold time.Duration
	cmd := &cobra.Command{
		Use:          "node-density-heavy",
		Short:        "Runs node-density-heavy workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			totalPods := workerNodeCount * podsPerNode
			podCount, err := discovery.GetCurrentPodCount()
			if err != nil {
				log.Fatal(err)
			}
			// We divide by two the number of pods to deploy to obtain the workload iterations
			jobIterations := (totalPods - podCount) / 2
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(jobIterations))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
		},
		Run: func(cmd *cobra.Command, args []string) {
			configSpec, err := config.Parse("node-density-heavy.yml", true)
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
	workerNodeCount, err := discovery.GetWorkerNodeCount()
	if err != nil {
		log.Fatal("Error obtaining worker node count:", err)
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 1*time.Hour, "Pod ready timeout threshold")
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	return cmd
}
