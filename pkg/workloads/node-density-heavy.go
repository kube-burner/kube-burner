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

	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density-heavy workload
func NewNodeDensityHeavy(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, probesPeriod int
	var podReadyThreshold time.Duration
	var extract bool
	cmd := &cobra.Command{
		Use:          "node-density-heavy",
		Short:        "Runs node-density-heavy workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			if extract {
				if err := wh.extractWorkload(cmd.Name()); err != nil {
					log.Fatal(err)
				}
				os.Exit(0)
			}
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
			os.Setenv("JOB_ITERATIONS", fmt.Sprint((totalPods-podCount)/2))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("PROBES_PERIOD", fmt.Sprint(probesPeriod))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name())
		},
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 1*time.Hour, "Pod ready timeout threshold")
	cmd.Flags().IntVar(&probesPeriod, "probes-period", 10, "Perf app readiness/livenes probes period in seconds")
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	cmd.Flags().BoolVar(&extract, "extract", false, "Extract workload in the current directory")
	return cmd
}
