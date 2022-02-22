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

package watcher

import (
	k8sv1 "k8s.io/api/core/v1"
	kvv1 "kubevirt.io/api/core/v1"
)

func getPodID(pod k8sv1.Pod) string {
	// in case the parent of the pod is a KubeVirt VM object, we use the VM identifier as the index
	if id, exists := pod.Labels["kubevirt-vm"]; exists {
		return id
	}

	if id, exists := pod.Labels["kubevirt.io/created-by"]; exists {
		return id
	}

	return string(pod.UID)
}

func getVMID(vm kvv1.VirtualMachine) string {
	return vm.Labels["kubevirt-vm"]
}

func getVMIID(vmi kvv1.VirtualMachineInstance) string {
	// in case the parent of the KubeVirt VMI is a KubeVirt VM object, we use the VM identifier as the index
	if id, exists := vmi.Labels["kubevirt-vm"]; exists {
		return id
	}
	return string(vmi.UID)
}
