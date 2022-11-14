package workloads

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density-heavy workload
func NewNodeDensityHeavy(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, probesPeriod int
	var podReadyThreshold time.Duration
	cmd := &cobra.Command{
		Use:          "node-density-heavy",
		Short:        "Runs node-density-heavy workload",
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
			// We divide by two the number of pods to deploy to obtain the workload iterations
			jobIterations := (totalPods - podCount) / 2
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(jobIterations))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("PROBES_PERIOD", fmt.Sprint(probesPeriod))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run("node-density-heavy.yml")
		},
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 1*time.Hour, "Pod ready timeout threshold")
	cmd.Flags().IntVar(&probesPeriod, "probes-period", 10, "Perf app readiness/livenes probes period in seconds")
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	return cmd
}
