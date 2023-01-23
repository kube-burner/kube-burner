// Copyright 2022 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workloads

import (
	"fmt"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"

	"github.com/spf13/cobra"
)

// NewClusterDensity holds cluster-density workload
func NewClusterDensity(wh *WorkloadHelper) *cobra.Command {
	var iterations, churnPercent int
	var churn, extract, networkPolicies bool
	var churnDelay, churnDuration time.Duration
	cmd := &cobra.Command{
		Use:   "cluster-density",
		Short: "Runs cluster-density workload",
		PreRun: func(cmd *cobra.Command, args []string) {
			if extract {
				if err := wh.extractWorkload(cmd.Name(), "metrics-aggregated.yml"); err != nil {
					log.Fatal(err)
				}
				os.Exit(0)
			}
			wh.Metadata.Benchmark = cmd.Name()
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(iterations))
			os.Setenv("CHURN", fmt.Sprint(churn))
			os.Setenv("CHURN_DURATION", fmt.Sprintf("%v", churnDuration))
			os.Setenv("CHURN_DELAY", fmt.Sprintf("%v", churnDelay))
			os.Setenv("CHURN_PERCENT", fmt.Sprint(churnPercent))
			os.Setenv("NETWORK_POLICIES", fmt.Sprint(networkPolicies))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), "metrics-aggregated.yml")
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", 0, "Cluster-density iterations")
	cmd.Flags().BoolVar(&churn, "churn", true, "Enable churning")
	cmd.Flags().DurationVar(&churnDuration, "churn-duration", 1*time.Hour, "Churn duration")
	cmd.Flags().DurationVar(&churnDelay, "churn-delay", 2*time.Minute, "Time to wait between each churn")
	cmd.Flags().IntVar(&churnPercent, "churn-percent", 10, "Percentage of job iterations that kube-burner will churn each round")
	cmd.Flags().BoolVar(&networkPolicies, "network-policies", true, "Enable network policies in the workload")
	cmd.Flags().BoolVar(&extract, "extract", false, "Extract workload in the current directory")
	cmd.MarkFlagRequired("iterations")
	return cmd
}
