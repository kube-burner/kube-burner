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

// VMMetric holds both pod, KubeVirt vmi and KubeVirt vm metrics. Note that not all condition timestamps will appear on the object.
type VMIMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	Timestamp time.Time `json:"timestamp"`

	// VMICreated represents the timestamp that the client request the creation of the VMI
	VMICreated *time.Time
	// VMIReadyLatency is the latency from VM creation till the VMICreated timestamp. It will only exist if the VMI was created by a VM.
	VMICreatedLatency int `json:"vmiCreatedLatency,omitempty"`

	//When a VirtualMachineInstance Object is first initialized and no phase, or Pending is present.
	VMIUnset *time.Time
	// VMIUnsetLatency is the latency from VM or VMI creation till the VMIUnset timestamp
	VMIUnsetLatency int `json:"vmiUnsetLatency,omitempty"`

	// Pending means the VirtualMachineInstance has been accepted by the system.
	VMIPending *time.Time
	// VMIPendingLatency is the latency from VM or VMI creation till the VMIPending timestamp
	VMIPendingLatency int `json:"vmiPendingLatency"`

	// A target Pod exists but is not yet scheduled and in running state.
	VMIScheduling *time.Time
	// VMISchedulingLatency is the latency from VM or VMI creation till the VMIScheduling timestamp
	VMISchedulingLatency int `json:"vmiSchedulingLatency"`

	// A target pod was scheduled and the system saw that Pod in runnig state.
	// Here is where the responsibility of virt-controller ends and virt-handler takes over.
	VMIScheduled *time.Time
	// VMIScheduledLatency is the latency from VM or VMI creation till the VMIScheduled timestamp
	VMIScheduledLatency int `json:"vmiScheduledLatency"`

	// Running means the pod has been bound to a node and the VirtualMachineInstance is started.
	VMIRunning *time.Time
	// VMIRunningLatency is the latency from VM or VMI creation till the VMIRunning timestamp
	VMIRunningLatency int `json:"vmiRunningLatency"`

	// Succeeded means that the VirtualMachineInstance stopped voluntarily, e.g. reacted to SIGTERM or shutdown was invoked from
	// inside the VirtualMachineInstance.
	VMISucceeded *time.Time
	// VMISucceededLatency is the latency from VM or VMI creation till the VMISucceeded timestamp
	VMISucceededLatency int `json:"vmiSucceededLatency,omitempty"`

	// Failed means that the vmi crashed, disappeared unexpectedly or got deleted from the cluster before it was ever started.
	VMIFailed *time.Time
	// VMIFailedLatency is the latency from VM or VMI creation till the VMIFailed timestamp
	VMIFailedLatency int `json:"vmiFailedLatency,omitempty"`

	// Unknown means that for some reason the state of the VirtualMachineInstance could not be obtained, typically due
	// to an error in communicating with the host of the VirtualMachineInstance.
	VMIUnknown *time.Time
	// VMIUnknownLatency is the latency from VM or VMI creation till the VMIUnknown timestamp
	VMIUnknownLatency int `json:"vmiUnknownLatency,omitempty"`

	// VMIProvisioning means, a VMI depends on DataVolumes which are in Pending/WaitForFirstConsumer status,
	// and some actions are taken to provision the PVCs for the DataVolumes
	VMIProvisioning *time.Time
	// VMIProvisioningLatency is the latency from VM or VMI creation till the VMIProvisioning timestamp
	VMIProvisioningLatency int `json:"vmiProvisioningLatency,omitempty"`

	// VMISynchronized reflects VirtualMachineInstance synchronize the libvirt Domain.
	VMISynchronized *time.Time
	// VMISynchronizedLatency is the latency from VM or VMI creation till the VMISynchronized timestamp
	VMISynchronizedLatency int `json:"vmiSynchronizedLatency,omitempty"`

	// VMIAgentConnected reflects whether the QEMU guest agent is connected through the channel
	VMIAgentConnected *time.Time
	// VMIAgentConnectedLatency is the latency from VM or VMI creation till the VMIAgentConnected timestamp
	VMIAgentConnectedLatency int `json:"vmiAgentConnectedLatency,omitempty"`

	// VMIAccessCredentialsSynchronized reflects whether the QEMU guest agent updated access credentials successfully
	VMIAccessCredentialsSynchronized *time.Time
	// VMIAccessCredentialsSynchronizedLatency is the latency from VM or VMI creation till the VMIAccessCredentialsSynchronized timestamp
	VMIAccessCredentialsSynchronizedLatency int `json:"vmiAccessCredentialsSynchronizedLatency,omitempty"`

	// VMIReady means the VMI is able to service requests and should be added to the load balancing pools of all matching services.
	VMIReady *time.Time
	// VMIReadyLatency is the latency from VM or VMI creation till the VMIReady timestamp
	VMIReadyLatency int `json:"vmiReadyLatency"`

	MetricName string `json:"metricName"`
	JobName    string `json:"jobName"`
	UUID       string `json:"uuid"`
	Namespace  string `json:"namespace"`
	Name       string `json:"vmiName"`
	NodeName   string `json:"nodeName"`
}
