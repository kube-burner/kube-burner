// Copyright 2026 The Kube-burner Authors.
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

package cluster

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
)

const (
	MetadataKeyK8sVersion = "k8sVersion"
	MetadataKeyTotalNodes = "totalNodes"
)

// Info contains generic Kubernetes cluster facts kube-burner core consumes.
type Info struct {
	K8sVersion string
	TotalNodes *int
}

// AsMetadata returns the generic Kubernetes cluster metadata keys kube-burner
// stamps onto indexed documents. Missing probe values are omitted so user
// metadata is not overwritten with empty values.
func (i Info) AsMetadata() map[string]any {
	metadata := make(map[string]any)
	if i.K8sVersion != "" {
		metadata[MetadataKeyK8sVersion] = i.K8sVersion
	}
	if i.TotalNodes != nil {
		metadata[MetadataKeyTotalNodes] = *i.TotalNodes
	}
	return metadata
}

// ApplyMetadata stamps discovered cluster metadata into m. A non-nil map is
// always returned.
func (i Info) ApplyMetadata(m map[string]any) map[string]any {
	if m == nil {
		m = make(map[string]any)
	}
	for k, v := range i.AsMetadata() {
		m[k] = v
	}
	return m
}

// Probe collects generic Kubernetes cluster metadata. Partial data is returned
// alongside an aggregate error when one or more discovery calls fail.
func Probe(ctx context.Context, client kubernetes.Interface) (Info, error) {
	var info Info
	var errs []error
	version, err := client.Discovery().ServerVersion()
	if err != nil {
		errs = append(errs, fmt.Errorf("discovering Kubernetes server version: %w", err))
	} else if version != nil {
		info.K8sVersion = version.GitVersion
	}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		errs = append(errs, fmt.Errorf("listing nodes: %w", err))
	} else if nodes == nil {
		errs = append(errs, fmt.Errorf("listing nodes: empty response"))
	} else {
		totalNodes := len(nodes.Items)
		info.TotalNodes = &totalNodes
	}
	return info, utilerrors.NewAggregate(errs)
}
