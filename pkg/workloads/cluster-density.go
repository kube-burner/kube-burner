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

	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/spf13/cobra"
)

// NewClusterDensity holds cluster-density workload
func NewClusterDensity(wh *WorkloadHelper, variant string) *cobra.Command {
	var iterations, churnPercent int
	var churn bool
	var churnDelay, churnDuration time.Duration
	cmd := &cobra.Command{
		Use:   variant,
		Short: fmt.Sprintf("Runs %v workload", variant),
		PreRun: func(cmd *cobra.Command, args []string) {
			if !burner.VerifyContainerRegistry(wh.restConfig) {
				os.Exit(1)
			}
			wh.Metadata.Benchmark = cmd.Name()
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(iterations))
			os.Setenv("CHURN", fmt.Sprint(churn))
			os.Setenv("CHURN_DURATION", fmt.Sprintf("%v", churnDuration))
			os.Setenv("CHURN_DELAY", fmt.Sprintf("%v", churnDelay))
			os.Setenv("CHURN_PERCENT", fmt.Sprint(churnPercent))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), MetricsProfileMap[cmd.Name()])
		},
	}
	cmd.Flags().IntVar(&iterations, "iterations", 0, fmt.Sprintf("%v iterations", variant))
	cmd.Flags().BoolVar(&churn, "churn", true, "Enable churning")
	cmd.Flags().DurationVar(&churnDuration, "churn-duration", 1*time.Hour, "Churn duration")
	cmd.Flags().DurationVar(&churnDelay, "churn-delay", 2*time.Minute, "Time to wait between each churn")
	cmd.Flags().IntVar(&churnPercent, "churn-percent", 10, "Percentage of job iterations that kube-burner will churn each round")
	cmd.MarkFlagRequired("iterations")
	return cmd
}
