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
	"sync"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
)

type object struct {
	config.Object
	gvr           schema.GroupVersionResource
	gvk           *schema.GroupVersionKind
	waitGVR       *schema.GroupVersionResource
	objectSpec    []byte
	namespace     string
	namespaced    bool
	ready         bool
	documentIndex int
	symbolicGVK   []*schema.GroupVersionKind
	// resolvedGVRs tracks GVRs resolved at runtime for templated kinds.
	// Key is the GVR string, value is the GVR itself.
	// This is needed because templated kinds produce different GVRs per iteration.
	resolvedGVRs    sync.Map
	IsKindTemplated bool
}

func newObject(obj config.Object, mapper *restmapper.DeferredDiscoveryRESTMapper, defaultAPIVersion string, embedCfg *fileutils.EmbedConfiguration) *object {
	if obj.APIVersion == "" {
		obj.APIVersion = defaultAPIVersion
	}

	if len(obj.LabelSelector) == 0 {
		log.Fatalf("Empty labelSelectors not allowed with: %s", obj.Kind)
	}

	gvk := schema.FromAPIVersionAndKind(obj.APIVersion, obj.Kind)
	var symbolicGVKs []*schema.GroupVersionKind

	mappings, err := mapper.RESTMappings(gvk.GroupKind())
	if err != nil {
		log.Fatal(err)
	}

	if len(mappings) == 0 {
		log.Fatalf("No Resources found for kind: %s", obj.Kind)
	}

	var mapping *meta.RESTMapping

	if len(mappings) == 1 {
		mapping = mappings[0]
	} else {
		log.Debugf("Found %d mappings for %s", len(mappings), obj.Kind)

		for _, m := range mappings {
			gv := m.Resource.GroupVersion()
			symbolicGVKs = append(symbolicGVKs, &schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: obj.Kind})
			if gv.String() == obj.APIVersion {
				mapping = m
			}
		}
	}

	var waitGVR *schema.GroupVersionResource
	if obj.WaitOptions.Kind != "" {
		if obj.WaitOptions.APIVersion == "" {
			obj.WaitOptions.APIVersion = obj.APIVersion
		}
		waitGVK := schema.FromAPIVersionAndKind(obj.WaitOptions.APIVersion, obj.WaitOptions.Kind)
		waitMapping, err := mapper.RESTMapping(waitGVK.GroupKind())
		if err != nil {
			log.Fatal(err)
		}
		waitGVR = &waitMapping.Resource
	}

	o := object{
		Object:      obj,
		gvr:         mapping.Resource,
		waitGVR:     waitGVR,
		namespaced:  mapping.Scope.Name() == meta.RESTScopeNameNamespace,
		gvk:         &gvk,
		symbolicGVK: symbolicGVKs,
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
