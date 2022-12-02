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

// NewNodeDensity holds node-density workload
func NewNodeDensity(wh *WorkloadHelper) *cobra.Command {
	var podsPerNode int
	var podReadyThreshold time.Duration
	var containerImage string
	var extract bool
	cmd := &cobra.Command{
		Use:          "node-density",
		Short:        "Runs node-density workload",
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			if extract {
				if err := wh.extractWorkload(cmd.Name()); err != nil {
					log.Fatal(err)
				}
				os.Exit(0)
			}
			wh.Metadata.Benchmark = cmd.Name()
			workerNodeCount, err := wh.discoveryAgent.GetWorkerNodeCount()
			if err != nil {
				log.Fatal("Error obtaining worker node count:", err)
			}
			totalPods := workerNodeCount * podsPerNode
			podCount, err := wh.discoveryAgent.GetCurrentPodCount()
			if err != nil {
				log.Fatal(err)
			}
			os.Setenv("JOB_ITERATIONS", fmt.Sprint(totalPods-podCount))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("CONTAINER_IMAGE", containerImage)
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name())
		},
	}
	cmd.Flags().IntVar(&podsPerNode, "pods-per-node", 245, "Pods per node")
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 5*time.Second, "Pod ready timeout threshold")
	cmd.Flags().StringVar(&containerImage, "container-image", "gcr.io/google_containers/pause:3.1", "Container image")
	cmd.Flags().BoolVar(&extract, "extract", false, "Extract workload in the current directory")
	return cmd
}
