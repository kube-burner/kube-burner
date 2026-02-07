// Copyright 2025 The Kube-burner Authors.
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

package measurements

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

// metadataTransformOptions configures which metadata fields to include in the minimal object
type metadataTransformOptions struct {
	includeNamespace       bool
	includeLabels          bool
	includeOwnerReferences bool
}

// createMinimalUnstructured creates a minimal unstructured object with only metadata fields needed for latency measurements.
// This is the common base used by all unstructured transform functions.
func createMinimalUnstructured(u *unstructured.Unstructured, opts metadataTransformOptions) *unstructured.Unstructured {
	metadata := map[string]interface{}{
		"name":              u.GetName(),
		"uid":               u.GetUID(),
		"creationTimestamp": u.GetCreationTimestamp(),
	}

	if opts.includeNamespace {
		metadata["namespace"] = u.GetNamespace()
	}
	if opts.includeLabels {
		metadata["labels"] = u.GetLabels()
	}
	if opts.includeOwnerReferences {
		metadata["ownerReferences"] = u.GetOwnerReferences()
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": u.GetAPIVersion(),
			"kind":       u.GetKind(),
			"metadata":   metadata,
		},
	}
}

// defaultMetadataTransformOpts returns the most common transform options (with namespace and labels)
func defaultMetadataTransformOpts() metadataTransformOptions {
	return metadataTransformOptions{
		includeNamespace: true,
		includeLabels:    true,
	}
}

// PodTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels, ownerReferences
// - spec: nodeName
// - status: conditions
func PodTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := createMinimalUnstructured(u, metadataTransformOptions{
			includeNamespace:       true,
			includeLabels:          true,
			includeOwnerReferences: true,
		})

		if nodeName, found, _ := unstructured.NestedString(u.Object, "spec", "nodeName"); found && nodeName != "" {
			_ = unstructured.SetNestedField(minimal.Object, nodeName, "spec", "nodeName")
		}
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}
