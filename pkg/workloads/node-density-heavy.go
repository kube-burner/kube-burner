package workloads

import (
	"fmt"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density-heavy workload
func NewNodeDensityHeavy(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, workerNodeCount int
	var podReadyThreshold time.Duration
	cmd := &cobra.Command{
		Use:          "node-density-heavy",
		Short:        "Runs node-density-heavy workload",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			metadata.Benchmark = cmd.Name()
			metadata.Benchmark = "node-density-heavy"
			totalPods := workerNodeCount * podsPerNode
			podCount, err := discovery.GetCurrentPodCount()
			if err != nil {
				return err
			}
			// We divide by two the number of pods to deploy to obtain the workload iterations
			jobIterations := (totalPods - podCount) / 2
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(jobIterations))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			err = run("-c", "node-density-heavy.yml", "-a", "alerts.yml")
			if err != nil {
				fmt.Println(err)
				metadata.Passed = false
			} else {
				metadata.Passed = true
			}
			return err
		},
	}
	workerNodeCount, err := discovery.GetWorkerNodeCount()
	if err != nil {
		fmt.Println("Error obtaining worker node count: ", err)
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 1*time.Hour, "Pod ready timeout threshold")
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	return cmd
}
