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

package util

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestAreNodesHealthyConditions(t *testing.T) {
	conditionTypes := []corev1.NodeConditionType{
		corev1.NodeReady,
		corev1.NodeMemoryPressure,
		corev1.NodeDiskPressure,
		corev1.NodePIDPressure,
		corev1.NodeNetworkUnavailable,
		"PortworxNewStorageNodeProvisioned",
		"OtherVendorCondition",
	}
	for _, conditionType := range conditionTypes {
		for _, status := range []corev1.ConditionStatus{corev1.ConditionTrue, corev1.ConditionFalse, corev1.ConditionUnknown} {
			t.Run(string(conditionType)+"/"+string(status), func(t *testing.T) {
				healthy := healthyNode("healthy")
				node := healthyNode("test")
				condition := corev1.NodeCondition{Type: conditionType, Status: status}
				found := false
				for i := range node.Status.Conditions {
					if node.Status.Conditions[i].Type == conditionType {
						node.Status.Conditions[i] = condition
						found = true
					}
				}
				if !found {
					node.Status.Conditions = append(node.Status.Conditions, condition)
				}
				want := true
				if conditionType == corev1.NodeReady {
					want = status == corev1.ConditionTrue
				} else if found {
					want = status == corev1.ConditionFalse
				}
				client := fake.NewClientset(healthy, node)
				if got := areNodesHealthy(context.Background(), client); got != want {
					t.Fatalf("areNodesHealthy() = %v, want %v", got, want)
				}
			})
		}
	}
}

func TestAreNodesHealthyVendorConditionDoesNotHideFault(t *testing.T) {
	node := healthyNode("test")
	node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
		Type: "PortworxNewStorageNodeProvisioned", Status: corev1.ConditionTrue,
	})
	node.Status.Conditions[1].Status = corev1.ConditionTrue
	client := fake.NewClientset(node)
	if areNodesHealthy(context.Background(), client) {
		t.Fatal("expected MemoryPressure=True to fail even with a positive vendor condition")
	}
}

func TestAreNodesHealthyListError(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("list", "nodes", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("cannot list nodes")
	})
	if areNodesHealthy(context.Background(), client) {
		t.Fatal("expected a node list error to fail the health check")
	}
}

func healthyNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
			{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
			{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse},
			{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionFalse},
		}},
	}
}
