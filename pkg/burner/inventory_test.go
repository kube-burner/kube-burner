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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
)

func TestResourceInventoryLookups(t *testing.T) {
	const (
		namespacesPath = "/api/v1/namespaces"
		testNamespace  = "workload"
		configMapsPath = namespacesPath + "/" + testNamespace + "/configmaps"
		namespaceBody  = `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"workload","uid":"ns-id"}}`
		configMapBody  = `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"generated-abc","namespace":"workload","uid":"cm-id"}}`
	)
	posts := 0
	httpClient := restfake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
		var status int
		var body string
		switch req.Method + " " + req.URL.Path {
		case "POST " + namespacesPath:
			status, body = http.StatusCreated, namespaceBody
		case "POST " + configMapsPath:
			posts++
			if posts == 1 {
				status, body = http.StatusCreated, configMapBody
			} else {
				status = http.StatusConflict
				body = `{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"AlreadyExists","code":409}`
			}
		case "GET " + namespacesPath + "/" + testNamespace:
			status, body = http.StatusOK, namespaceBody
		case "GET " + configMapsPath + "/generated-abc":
			status, body = http.StatusOK, configMapBody
		default:
			return nil, fmt.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	restConfig := &rest.Config{Host: "https://example.test"}
	client, err := dynamic.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	clientSet, err := kubernetes.NewForConfigAndClient(restConfig, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	inventory := &resourceInventory{}
	ex := JobExecutor{
		Job:               config.Job{Name: "test-job"},
		dynamicClient:     client,
		clientSet:         clientSet,
		createdNamespaces: map[string]bool{},
		inventory:         inventory,
	}
	ctx := t.Context()
	ex.createNamespace(testNamespace, nil, nil)
	configMaps := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"}}
	obj.SetGenerateName("generated-")
	ex.createRequest(ctx, configMaps, testNamespace, obj, time.Minute)
	obj.SetName("already-exists")
	ex.createRequest(ctx, configMaps, testNamespace, obj, time.Minute)

	dir := t.TempDir()
	spec := config.Spec{MetricsEndpoints: []config.MetricsEndpoint{
		{IndexerConfig: indexers.IndexerConfig{Type: indexers.LocalIndexer, MetricsDirectory: dir}},
	}}
	if err := inventory.write(spec); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, metrics.ResourceInventoryFilename))
	if err != nil {
		t.Fatal(err)
	}
	var resources []ResourceReference
	if err := json.Unmarshal(data, &resources); err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected two created resources, got %+v", resources)
	}
	for _, ref := range resources {
		resource := client.Resource(schema.GroupVersionResource(ref.GVR))
		var getter dynamic.ResourceInterface = resource
		if ref.Namespace != "" {
			getter = resource.Namespace(ref.Namespace)
		}
		obj, err := getter.Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if obj.GetUID() != ref.UID || ref.Job != ex.Name {
			t.Errorf("unexpected resource reference %+v for object with UID %q", ref, obj.GetUID())
		}
	}
}
