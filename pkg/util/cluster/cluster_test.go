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
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

const (
	testK8sVersion   = "v1.35.3"
	testDistribution = "microshift"
)

func TestAsMetadataEmitsOnlyDiscoveredValues(t *testing.T) {
	metadata := Info{}.AsMetadata()
	if len(metadata) != 0 {
		t.Fatalf("expected empty metadata, got %v", metadata)
	}

	nodes := 0
	metadata = Info{TotalNodes: &nodes}.AsMetadata()
	if len(metadata) != 1 {
		t.Fatalf("expected one metadata key, got %v", metadata)
	}
	if metadata[MetadataKeyTotalNodes] != 0 {
		t.Fatalf("expected totalNodes=0, got %v", metadata[MetadataKeyTotalNodes])
	}

	nodes = 3
	metadata = Info{K8sVersion: testK8sVersion, TotalNodes: &nodes}.AsMetadata()
	expected := map[string]any{
		MetadataKeyK8sVersion: testK8sVersion,
		MetadataKeyTotalNodes: 3,
	}
	if len(metadata) != len(expected) {
		t.Fatalf("expected %d metadata keys, got %d: %v", len(expected), len(metadata), metadata)
	}
	for key, want := range expected {
		if metadata[key] != want {
			t.Fatalf("expected %s=%v, got %v", key, want, metadata[key])
		}
	}
	for _, absent := range []string{"distribution", "microshift", "microshiftVersion", "microshiftMajorVersion", "platform", "openshift", "ocpVersion"} {
		if _, ok := metadata[absent]; ok {
			t.Fatalf("did not expect %q in kube-burner core metadata", absent)
		}
	}
}

func TestApplyMetadata(t *testing.T) {
	nodes := 2
	input := map[string]any{
		MetadataKeyK8sVersion: "stale",
		MetadataKeyTotalNodes: 99,
		"distribution":        testDistribution,
		"customKey":           "preserve-me",
	}

	got := (Info{K8sVersion: testK8sVersion, TotalNodes: &nodes}).ApplyMetadata(input)
	got["sentinel"] = "same-map"
	if input["sentinel"] != "same-map" {
		t.Fatal("expected ApplyMetadata to mutate the existing map instance")
	}
	if input[MetadataKeyK8sVersion] != testK8sVersion {
		t.Fatalf("expected k8sVersion to be overwritten, got %v", input[MetadataKeyK8sVersion])
	}
	if input[MetadataKeyTotalNodes] != 2 {
		t.Fatalf("expected totalNodes to be overwritten, got %v", input[MetadataKeyTotalNodes])
	}
	if input["distribution"] != testDistribution {
		t.Fatalf("expected unrelated distribution metadata to be preserved, got %v", input["distribution"])
	}
	if input["customKey"] != "preserve-me" {
		t.Fatalf("expected custom metadata to be preserved, got %v", input["customKey"])
	}
}

func TestApplyMetadataNilInput(t *testing.T) {
	nodes := 1
	metadata := (Info{K8sVersion: testK8sVersion, TotalNodes: &nodes}).ApplyMetadata(nil)
	if metadata == nil {
		t.Fatal("expected ApplyMetadata to allocate a map for nil input")
	}
	if metadata[MetadataKeyK8sVersion] != testK8sVersion {
		t.Fatalf("expected k8sVersion metadata, got %v", metadata[MetadataKeyK8sVersion])
	}
	if metadata[MetadataKeyTotalNodes] != 1 {
		t.Fatalf("expected totalNodes metadata, got %v", metadata[MetadataKeyTotalNodes])
	}
}

func TestApplyMetadataDoesNotOverwriteWithAbsentValues(t *testing.T) {
	input := map[string]any{
		MetadataKeyK8sVersion: "user-version",
		MetadataKeyTotalNodes: 7,
	}
	got := (Info{}).ApplyMetadata(input)
	if got[MetadataKeyK8sVersion] != "user-version" {
		t.Fatalf("expected k8sVersion to be preserved, got %v", got[MetadataKeyK8sVersion])
	}
	if got[MetadataKeyTotalNodes] != 7 {
		t.Fatalf("expected totalNodes to be preserved, got %v", got[MetadataKeyTotalNodes])
	}
}

func TestProbe(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}},
	)
	discovery := fakeDiscovery(t, client)
	discovery.FakedServerVersion = &version.Info{GitVersion: testK8sVersion}

	info, err := Probe(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.K8sVersion != testK8sVersion {
		t.Fatalf("expected k8sVersion v1.35.3, got %q", info.K8sVersion)
	}
	if info.TotalNodes == nil {
		t.Fatal("expected totalNodes to be discovered")
	}
	if *info.TotalNodes != 2 {
		t.Fatalf("expected totalNodes=2, got %d", *info.TotalNodes)
	}
}

func TestProbeReturnsPartialInfoWithAggregatedErrors(t *testing.T) {
	client := k8sfake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
	)
	discovery := fakeDiscovery(t, client)
	versionErr := errors.New("version unavailable")
	discovery.PrependReactor("get", "version", func(action kubetesting.Action) (bool, runtime.Object, error) {
		return true, nil, versionErr
	})

	info, err := Probe(context.Background(), client)
	if err == nil {
		t.Fatal("expected partial probe error")
	}
	if !errors.Is(err, versionErr) {
		t.Fatalf("expected aggregate to contain version error, got %v", err)
	}
	if info.K8sVersion != "" {
		t.Fatalf("expected k8sVersion to be absent after version error, got %q", info.K8sVersion)
	}
	if info.TotalNodes == nil || *info.TotalNodes != 1 {
		t.Fatalf("expected totalNodes=1 despite version error, got %v", info.TotalNodes)
	}
}

func fakeDiscovery(t *testing.T, client *k8sfake.Clientset) *fakediscovery.FakeDiscovery {
	t.Helper()
	discovery, ok := client.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("unexpected discovery type")
	}
	return discovery
}
