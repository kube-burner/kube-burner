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

// ServiceTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels, annotations (kube-burner.io/service-latency only)
// - spec: type, clusterIPs, ports
// - status: loadBalancer.ingress
func ServiceTransformFunc() cache.TransformFunc {
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

		// Preserve only kube-burner annotation if present
		if annotations := u.GetAnnotations(); annotations != nil {
			if val, exists := annotations["kube-burner.io/service-latency"]; exists {
				_ = unstructured.SetNestedField(minimal.Object, map[string]interface{}{
					"kube-burner.io/service-latency": val,
				}, "metadata", "annotations")
			}
		}

		// Preserve spec.type, spec.clusterIPs, spec.ports
		if svcType, found, _ := unstructured.NestedString(u.Object, "spec", "type"); found {
			_ = unstructured.SetNestedField(minimal.Object, svcType, "spec", "type")
		}
		if clusterIPs, found, _ := unstructured.NestedStringSlice(u.Object, "spec", "clusterIPs"); found {
			ips := make([]interface{}, len(clusterIPs))
			for i, ip := range clusterIPs {
				ips[i] = ip
			}
			_ = unstructured.SetNestedSlice(minimal.Object, ips, "spec", "clusterIPs")
		}
		if ports, found, _ := unstructured.NestedSlice(u.Object, "spec", "ports"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, ports, "spec", "ports")
		}

		// Preserve status.loadBalancer.ingress
		if ingress, found, _ := unstructured.NestedSlice(u.Object, "status", "loadBalancer", "ingress"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, ingress, "status", "loadBalancer", "ingress")
		}

		return minimal, nil
	}
}

// PVCTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: phase, conditions
func PVCTransformFunc() cache.TransformFunc {
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

		// Preserve status.phase (needed for Bound detection)
		if phase, found, _ := unstructured.NestedString(u.Object, "status", "phase"); found {
			_ = unstructured.SetNestedField(minimal.Object, phase, "status", "phase")
		}

		// Preserve status.conditions
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}

// DataVolumeTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: conditions
func DataVolumeTransformFunc() cache.TransformFunc {
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

		// Preserve status.conditions (needed for Bound, Running, Ready detection)
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}

// NetworkPolicyTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp
func NetworkPolicyTransformFunc() cache.TransformFunc {
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
				},
			},
		}

		return minimal, nil
	}
}

// VolumeSnapshotTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: readyToUse, creationTime
func VolumeSnapshotTransformFunc() cache.TransformFunc {
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

		// Preserve status.readyToUse (needed for Ready detection)
		if readyToUse, found, _ := unstructured.NestedBool(u.Object, "status", "readyToUse"); found {
			_ = unstructured.SetNestedField(minimal.Object, readyToUse, "status", "readyToUse")
		}

		// Preserve status.creationTime
		if creationTime, found, _ := unstructured.NestedFieldNoCopy(u.Object, "status", "creationTime"); found {
			_ = unstructured.SetNestedField(minimal.Object, creationTime, "status", "creationTime")
		}

		return minimal, nil
	}
}

// VirtualMachineTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: conditions
func VirtualMachineTransformFunc() cache.TransformFunc {
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

		// Preserve status.conditions (needed for Ready detection)
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}

// VirtualMachineInstanceTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels, ownerReferences
// - status: phase
func VirtualMachineInstanceTransformFunc() cache.TransformFunc {
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
					"ownerReferences":   u.GetOwnerReferences(),
				},
			},
		}

		// Preserve status.phase (needed for Pending, Scheduling, Scheduled, Running detection)
		if phase, found, _ := unstructured.NestedString(u.Object, "status", "phase"); found {
			_ = unstructured.SetNestedField(minimal.Object, phase, "status", "phase")
		}

		return minimal, nil
	}
}

// VirtualMachineInstanceMigrationTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - spec: vmiName
// - status: phaseTransitionTimestamps
func VirtualMachineInstanceMigrationTransformFunc() cache.TransformFunc {
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

		// Preserve spec.vmiName
		if vmiName, found, _ := unstructured.NestedString(u.Object, "spec", "vmiName"); found {
			_ = unstructured.SetNestedField(minimal.Object, vmiName, "spec", "vmiName")
		}

		// Preserve status.phaseTransitionTimestamps (needed for all phase latencies)
		if timestamps, found, _ := unstructured.NestedSlice(u.Object, "status", "phaseTransitionTimestamps"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, timestamps, "status", "phaseTransitionTimestamps")
		}

		return minimal, nil
	}
}
