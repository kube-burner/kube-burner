// Copyright 2023 The Kube-burner Authors.
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

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// NewClusterMaximums holds cluster-density workload
func NewClusterMaximums(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode, namespaces, backends, crds int
	const podsPerIter = 4
	cmd := &cobra.Command{
		Use:   "cluster-maximums",
		Short: "Runs cluster-maximums workload",
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			totalPods := wh.Metadata.WorkerNodesCount * podsPerNode
			podCount, err := wh.OcpMetaAgent.GetCurrentPodCount()
			if err != nil {
				log.Fatal(err)
			}
			os.Setenv("JOB_ITERATIONS", fmt.Sprint((totalPods-podCount)/podsPerIter))
			os.Setenv("NAMESPACES", fmt.Sprint(namespaces))
			os.Setenv("BACKENDS", fmt.Sprint(namespaces))
			os.Setenv("CRDS", fmt.Sprint(namespaces))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), MetricsProfileMap[cmd.Name()])
		},
	}
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 1000, "Pods per node")
	cmd.Flags().IntVar(&namespaces, "namespaces", 10000, "Number of iterations. max-namespaces")
	cmd.Flags().IntVar(&backends, "backends", 5000, "Number of backends per service. max-backends")
	cmd.Flags().IntVar(&crds, "crds", 1024, "Number of CRDs. max-crds")
	cmd.MarkFlagRequired("iterations")
	return cmd
}
