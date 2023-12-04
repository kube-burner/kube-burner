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

	"github.com/spf13/cobra"
)

// NewClusterDensity holds cluster-density workload
func NewWebBurner(wh *WorkloadHelper, variant string) *cobra.Command {
	var limitcount, scale int
	var bfd, crd, probe, sriov bool
	var bridge string
	var podReadyThreshold time.Duration
	cmd := &cobra.Command{
		Use:   variant,
		Short: fmt.Sprintf("Runs %v workload", variant),
		PreRun: func(cmd *cobra.Command, args []string) {
			wh.Metadata.Benchmark = cmd.Name()
			os.Setenv("BFD", fmt.Sprint(bfd))
			os.Setenv("BRIDGE", fmt.Sprint(bridge))
			os.Setenv("CRD", fmt.Sprintf("%v", crd))
			os.Setenv("LIMITCOUNT", fmt.Sprint(limitcount))
			os.Setenv("POD_READY_THRESHOLD", fmt.Sprintf("%v", podReadyThreshold))
			os.Setenv("PROBE", fmt.Sprint(probe))
			os.Setenv("SCALE", fmt.Sprint(scale))
			os.Setenv("SRIOV", fmt.Sprint(sriov))
		},
		Run: func(cmd *cobra.Command, args []string) {
			wh.run(cmd.Name(), MetricsProfileMap[cmd.Name()])
		},
	}
	cmd.Flags().DurationVar(&podReadyThreshold, "pod-ready-threshold", 2*time.Minute, "Pod ready timeout threshold")
	cmd.Flags().IntVar(&limitcount, "limitcount", 1, "Limitcount")
	cmd.Flags().IntVar(&scale, "scale", 1, "Scale")
	cmd.Flags().BoolVar(&bfd, "bfd", true, "Enable BFD")
	cmd.Flags().BoolVar(&crd, "crd", true, "Enable AdminPolicyBasedExternalRoute CR")
	cmd.Flags().BoolVar(&probe, "probe", false, "Enable readiness probes")
	cmd.Flags().BoolVar(&sriov, "sriov", true, "Enable SRIOV")
	cmd.Flags().StringVar(&bridge, "bridge", "br-ex", "Data-plane bridge")
	return cmd
}
