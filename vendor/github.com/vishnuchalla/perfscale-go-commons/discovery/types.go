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

package discovery

type InfraObj struct {
	Status struct {
		InfrastructureName string `json:"infrastructureName"`
		Platform           string `json:"platform"`
	} `json:"status"`
}

type VersionObj struct {
	OcpVersion string
	K8sVersion string
}

type clusterVersion struct {
	Status struct {
		History []struct {
			State   string `json:"state"`
			Version string `json:"version"`
		} `json:"history"`
	} `json:"status"`
}

type NodeInfo struct {
	WorkerCount int
	InfraCount  int
	TotalNodes  int
	MasterType  string
	WorkerType  string
	InfraType   string
}

const (
	routeGroup         = "route.openshift.io"
	routeVersion       = "v1"
	routeResource      = "routes"
	completedUpdate    = "Completed"
	workerNodeSelector = "node-role.kubernetes.io/worker=,node-role.kubernetes.io/infra!=,node-role.kubernetes.io/workload!="
)
