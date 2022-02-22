// Copyright 2021 The Kube-burner Authors.
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

// PodMetric is a structure that contains detailed information of the pod status
type PodMetric struct {
	Timestamp time.Time `json:"timestamp"`

	// PodCreated represents the timestamp that the client request the creation of the pod object
	PodCreated *time.Time
	// PodCreatedLatency is the latency from the creation of the object that created the pod till the PodCreated timestamp.
	// It will only exist if the pod was created by a another object.
	PodCreatedLatency int `json:"podCreatedLatency,omitempty"`

	// PodScheduled represents the timestamp when the pod was scheduled to a node.
	PodScheduled *time.Time
	// PodScheduledLatency is the latency the creation of the object that created the pod or the pod creation till the PodScheduled timestamp.
	PodScheduledLatency int `json:"podScheduledLatency"`

	// PodContainersReady represents the timestamp whether all containers in the pod are ready.
	PodContainersReady *time.Time
	// PodContainersReadyLatency is the latency the creation of the object that created the pod or the pod creation till the PodContainersReady timestamp.
	PodContainersReadyLatency int `json:"podContainersReadyLatency"`

	// PodInitialized represents the timestamp when all init containers in the pod have started successfully..
	PodInitialized *time.Time
	// PodInitializedLatency is the latency the creation of the object that created the pod or the pod creation till the PodInitialized timestamp.
	PodInitializedLatency int `json:"podInitializedLatency"`

	// PodReady represents the timestamp when the pod is able to service requests and should be added to the
	// load balancing pools of all matching services.
	PodReady *time.Time
	// PodReadyLatency is the latency the creation of the object that created the pod or the pod creation till the PodReady timestamp.
	PodReadyLatency int `json:"podReadyLatency"`

	MetricName string `json:"metricName"`
	JobName    string `json:"jobName"`
	UUID       string `json:"uuid"`
	Namespace  string `json:"namespace"`
	Name       string `json:"podName"`
	NodeName   string `json:"nodeName"`
}
