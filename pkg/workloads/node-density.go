package workloads

import (
	"fmt"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density workload
func NewNodeDensity(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, workerNodeCount int
	var podReadyThreshold time.Duration
	var containerImage string
	cmd := &cobra.Command{
		Use:          "node-density <flags>",
		Short:        "Runs node-density workload",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			metadata.Benchmark = "node-density"
			totalPods := workerNodeCount * podsPerNode
			podCount, err := discovery.GetCurrentPodCount()
			if err != nil {
				return err
			}
			jobIterations := totalPods - podCount
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(jobIterations))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("CONTAINER_IMAGE", containerImage)
			err = run("-c", "node-density.yml", "-a", "node-density-alerts.yml")
			if err != nil {
				fmt.Println(err)
				metadata.Passed = false
			} else {
				metadata.Passed = true
			}
			wh.indexMetadata()
			return err
		},
	}
	workerNodeCount, err := discovery.GetWorkerNodeCount()
	if err != nil {
		fmt.Println("Error obtaining worker node count: ", err)
	}
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 5*time.Second, "Pod ready timeout threshold")
	cmd.Flags().StringVar(&containerImage, "container-image", "gcr.io/google_containers/pause:3.1", "Container image")
	return cmd
}
