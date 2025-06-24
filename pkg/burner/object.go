// Copyright 2024 The Kube-burner Authors.
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
	"io"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
)

type object struct {
	config.Object
	gvr             schema.GroupVersionResource
	objectSpec      []byte
	namespace       string
	namespaced      bool
	ready           bool
	deferredMapping bool
	gvk             schema.GroupVersionKind
}

func newObject(obj config.Object, mapper meta.RESTMapper, defaultAPIVersion string, embedCfg *fileutils.EmbedConfiguration) *object {
	if obj.APIVersion == "" {
		obj.APIVersion = defaultAPIVersion
	}

	if len(obj.LabelSelector) == 0 {
		log.Fatalf("Empty labelSelectors not allowed with: %s", obj.Kind)
	}
	gvk := schema.FromAPIVersionAndKind(obj.APIVersion, obj.Kind)
	mapping, err := mapper.RESTMapping(gvk.GroupKind())
	o := object{
		Object: obj,
		gvk:    gvk,
	}
	if err != nil {
		log.Debugf("REST mapping failed for %s, will use deferred mapping: %v", gvk.Kind, err)
		o.deferredMapping = true
		o.namespaced = true
	} else {
		o.gvr = mapping.Resource
		o.namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
		o.deferredMapping = false
	}

	if obj.ObjectTemplate != "" {
		log.Debugf("Rendering template: %s", obj.ObjectTemplate)
		f, err := fileutils.GetWorkloadReader(obj.ObjectTemplate, embedCfg)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", obj.ObjectTemplate, err)
		}
		t, err := io.ReadAll(f)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", obj.ObjectTemplate, err)
		}
		o.objectSpec = t
	}

	return &o
}

func (o *object) resolveDeferredMapping(mapper meta.RESTMapper) error {
	if !o.deferredMapping {
		return nil
	}
	mapping, err := mapper.RESTMapping(o.gvk.GroupKind())
	if err != nil {
		return err
	}
	// updating the object
	o.gvr = mapping.Resource
	o.namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
	o.deferredMapping = false

	log.Debugf("Successfully resolved deferred mapping for %s", o.gvk.Kind)
	return nil
}
