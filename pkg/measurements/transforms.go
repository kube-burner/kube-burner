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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

// EventTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid
// - involvedObject: uid
// - reason, eventTime
func EventTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		event, ok := obj.(*corev1.Event)
		if !ok {
			return obj, nil
		}

		// Create minimal event with only fields needed for latency measurement
		minimal := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      event.Name,
				Namespace: event.Namespace,
				UID:       event.UID,
			},
			InvolvedObject: corev1.ObjectReference{
				UID: event.InvolvedObject.UID,
			},
			Reason:    event.Reason,
			EventTime: event.EventTime,
		}

		return minimal, nil
	}
}

// PodTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - spec: nodeName
// - status: conditions
func PodTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		// Create minimal pod with only fields needed for latency measurement
		minimal := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": u.GetAPIVersion(),
				"kind":       u.GetKind(),
				"metadata": map[string]interface{}{
					"name":              u.GetName(),
					"namespace":         u.GetNamespace(),
					"uid":               u.GetUID(),
					"creationTimestamp": u.GetCreationTimestamp(),
					"labels":            u.GetLabels(),
				},
			},
		}

		// Preserve spec.nodeName (needed for NodeName in metrics)
		if nodeName, found, _ := unstructured.NestedString(u.Object, "spec", "nodeName"); found && nodeName != "" {
			_ = unstructured.SetNestedField(minimal.Object, nodeName, "spec", "nodeName")
		}

		// Preserve status.conditions (needed for latency calculations)
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}

// JobTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: conditions, startTime
func JobTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": u.GetAPIVersion(),
				"kind":       u.GetKind(),
				"metadata": map[string]interface{}{
					"name":              u.GetName(),
					"namespace":         u.GetNamespace(),
					"uid":               u.GetUID(),
					"creationTimestamp": u.GetCreationTimestamp(),
					"labels":            u.GetLabels(),
				},
			},
		}

		// Preserve status.conditions (needed for JobComplete detection)
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		// Preserve status.startTime (needed for StartTimeLatency)
		if startTime, found, _ := unstructured.NestedString(u.Object, "status", "startTime"); found {
			_ = unstructured.SetNestedField(minimal.Object, startTime, "status", "startTime")
		}

		return minimal, nil
	}
}

// NodeTransformFunc preserves the following fields for latency measurements:
// - metadata: name, uid, creationTimestamp, labels
// - status: conditions
func NodeTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		minimal := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": u.GetAPIVersion(),
				"kind":       u.GetKind(),
				"metadata": map[string]interface{}{
					"name":              u.GetName(),
					"uid":               u.GetUID(),
					"creationTimestamp": u.GetCreationTimestamp(),
					"labels":            u.GetLabels(),
				},
			},
		}

		// Preserve status.conditions (needed for Ready, MemoryPressure, etc.)
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}
