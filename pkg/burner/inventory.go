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

package burner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// ResourceReference identifies one Kubernetes object created by a workload job.
type ResourceReference struct {
	GVR       metav1.GroupVersionResource `json:"gvr"`
	Namespace string                      `json:"namespace"`
	Name      string                      `json:"name"`
	UID       types.UID                   `json:"uid"`
	Job       string                      `json:"job"`
}

type resourceInventory struct {
	mu        sync.Mutex
	resources []ResourceReference
	sealed    bool
}

func (i *resourceInventory) record(job string, gvr schema.GroupVersionResource, obj metav1.Object) {
	if i == nil {
		return
	}
	ref := ResourceReference{
		GVR:       metav1.GroupVersionResource(gvr),
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		UID:       obj.GetUID(),
		Job:       job,
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.sealed {
		i.resources = append(i.resources, ref)
	}
}

func (i *resourceInventory) write(spec config.Spec) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	i.sealed = true
	resources := i.resources
	i.mu.Unlock()
	if resources == nil {
		resources = []ResourceReference{}
	}

	defaultDir := "collected-metrics-" + spec.GlobalConfig.UUID
	dirs := make(map[string]struct{})
	for _, endpoint := range spec.MetricsEndpoints {
		if util.IsLocalIndexer(endpoint.Type) {
			dir := endpoint.MetricsDirectory
			if dir == "" || dir == "collected-metrics-{{.UUID}}" {
				dir = defaultDir
			}
			dirs[filepath.Clean(dir)] = struct{}{}
		}
	}
	if len(dirs) == 0 {
		dirs[defaultDir] = struct{}{}
	}

	var errs []error
	for dir := range dirs {
		path := filepath.Join(dir, metrics.ResourceInventoryFilename)
		if err := writeResourceInventory(path, resources); err != nil {
			errs = append(errs, fmt.Errorf("writing resource inventory %s: %w", path, err))
			continue
		}
		log.Infof("Resource inventory written to %s (%d resources)", path, len(resources))
	}
	return errors.Join(errs...)
}

func writeResourceInventory(path string, resources []ResourceReference) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".resource-inventory-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encodeErr := json.NewEncoder(file).Encode(resources)
	if err := errors.Join(encodeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
