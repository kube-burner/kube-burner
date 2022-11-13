package workloads

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewClusterDensity holds cluster-density workload
func NewClusterDensity(wh *WorkloadHelper) *cobra.Command {
	var iterations int
	cmd := &cobra.Command{
		Use:   "cluster-density <flags>",
		Short: "Runs cluster-density workload",
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(iterations))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run("cluster-density.yml")
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", 0, "Cluster-density iterations")
	cmd.MarkFlagRequired("iterations")
	return cmd
}
