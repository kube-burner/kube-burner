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
		RunE: func(cmd *cobra.Command, args []string) error {
			metadata.Benchmark = "cluster-density"
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(iterations))
			err := run("-c", "cluster-density.yml", "-a", "alerts.yml")
			if err != nil {
				fmt.Println(err)
				metadata.Passed = false
			} else {
				metadata.Passed = true
			}
			return err
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", 0, "Cluster-density iterations")
	cmd.MarkFlagRequired("iterations")
	return cmd
}
