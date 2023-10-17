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

// NewNodeDensity holds node-density-cni workload
func NewNodeDensityCNI(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode int
	var namespacedIterations bool
	var podReadyThreshold time.Duration
	var iterationsPerNamespace int
	cmd := &cobra.Command{
		Use:          "node-density-cni",
		Short:        "Runs node-density-cni workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			totalPods := wh.Metadata.WorkerNodesCount * podsPerNode
			podCount, err := wh.OcpMetaAgent.GetCurrentPodCount()
			if err != nil {
				log.Fatal(err)
			}
			os.Setenv("JOB_ITERATIONS", fmt.Sprint((totalPods-podCount)/2))
			os.Setenv("NAMESPACED_ITERATIONS", fmt.Sprint(namespacedIterations))
			os.Setenv("ITERATIONS_PER_NAMESPACE", fmt.Sprint(iterationsPerNamespace))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), MetricsProfileMap[cmd.Name()])
		},
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 1*time.Minute, "Pod ready timeout threshold")
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	cmd.Flags().BoolVar(&namespacedIterations, "namespaced-iterations", true, "Namespaced iterations")
	cmd.Flags().IntVar(&iterationsPerNamespace, "iterations-per-namespace", 1000, "Iterations per namespace")
	return cmd
}
