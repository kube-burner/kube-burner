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

package types

import (
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// OpenShift Build CRD
	OpenShiftBuildGroup      = "build.openshift.io"
	OpenShiftBuildAPIVersion = "v1"
	OpenShiftBuildResource   = "builds"

	// Kubevirt CRD
	KubevirtGroup                            = "kubevirt.io"
	KubevirtAPIVersion                       = "v1"
	VirtualMachineResource                   = "virtualmachines"
	VirtualMachineInstanceResource           = "virtualmachineinstances"
	VirtualMachineInstanceReplicaSetResource = "virtualmachineinstancereplicasets"
)

type Object struct {
	Gvr            schema.GroupVersionResource
	ObjectTemplate string
	ObjectSpec     []byte
	Replicas       int
	InputVars      map[string]interface{}
	LabelSelector  map[string]string
	PatchType      string
	Namespaced     bool
	Kind           string
}

// Executor contains the information required to execute a job
type Executor struct {
	Objects   []Object
	Start     time.Time
	End       time.Time
	Config    config.Job
	Selector  *util.Selector
	UUID      string
	Limiter   *rate.Limiter
	NsObjects bool
}

// Condition contains details for the current condition of this pod.
type Condition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// UnstructuredContent minimum unstructured object content to unmarshal the status phase of a CRD
type UnstructuredContent struct {
	// Spec represents the unstructured object status
	Spec struct {
		Replicas int32 `json:"replicas,omitempty"`
	} `json:"spec"`

	// Status represents the unstructured object status
	Status struct {
		Phase         string      `json:"phase"`
		ReadyReplicas int32       `json:"readyReplicas,omitempty"`
		Conditions    []Condition `json:"conditions,omitempty"`
	} `json:"status"`
}
