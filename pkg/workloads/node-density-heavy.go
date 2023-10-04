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

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

// NewNodeDensity holds node-density-heavy workload
func NewNodeDensityHeavy(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode int
	var podReadyThreshold, probesPeriod time.Duration
	var namespacedIterations bool
	var iterationsPerNamespace int
	cmd := &cobra.Command{
		Use:          "node-density-heavy",
		Short:        "Runs node-density-heavy workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			totalPods := wh.Metadata.WorkerNodesCount * podsPerNode
			podCount, err := wh.OcpMetaAgent.GetCurrentPodCount()
			if err != nil {
				log.Fatal(err)
			}
			// We divide by two the number of pods to deploy to obtain the workload iterations
			os.Setenv("JOB_ITERATIONS", fmt.Sprint((totalPods-podCount)/2))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("PROBES_PERIOD", fmt.Sprint(probesPeriod.Seconds()))
			os.Setenv("NAMESPACED_ITERATIONS", fmt.Sprint(namespacedIterations))
			os.Setenv("ITERATIONS_PER_NAMESPACE", fmt.Sprint(iterationsPerNamespace))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), MetricsProfileMap[cmd.Name()])
		},
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 1*time.Hour, "Pod ready timeout threshold")
	cmd.Flags().DurationVar(&probesPeriod, "probes-period", 10*time.Second, "Perf app readiness/livenes probes period")
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	cmd.Flags().BoolVar(&namespacedIterations, "namespaced-iterations", true, "Namespaced iterations")
	cmd.Flags().IntVar(&iterationsPerNamespace, "iterations-per-namespace", 1000, "Iterations per namespace")
	return cmd
}
