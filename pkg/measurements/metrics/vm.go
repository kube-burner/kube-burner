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

package metrics

import (
	"time"
)

// VMMetric holds both pod, KubeVirt vmi and KubeVirt vm metrics
type VMMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	Timestamp time.Time `json:"timestamp"`

	// VMReady indicates the timestamp that the virtual machine is running and ready
	VMReady *time.Time
	// VMIReadyLatency is the latency from VM creation till the VMReady timestamp
	VMReadyLatency int `json:"vmReadyLatency,omitempty"`

	MetricName string `json:"metricName"`
	JobName    string `json:"jobName"`
	UUID       string `json:"uuid"`
	Namespace  string `json:"namespace"`
	Name       string `json:"vmName"`
	NodeName   string `json:"nodeName"`
}
