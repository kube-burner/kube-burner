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

package config

import (
	"strings"
	"testing"
)

func TestNameFromConfigPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"cluster-density.yml", "cluster-density"},
		{"workloads/node-density/node-density.yaml", "node-density"},
		{"/tmp/api-intensive.json", "api-intensive"},
		{"config", "config"},
		{"https://example.com/configs/cluster-density-v2.yml?ref=main", "cluster-density-v2"},
		{"http://example.com/kube-burner.yml", "kube-burner"},
		{"https://example.com/", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NameFromConfigPath(tt.path); got != tt.want {
			t.Errorf("NameFromConfigPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestParseGlobalName(t *testing.T) {
	cfg := `
global:
  name: my-workload
jobs: []
`
	spec, err := ParseWithUserdata("uuid", 0, strings.NewReader(cfg), nil, false, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.GlobalConfig.Name != "my-workload" {
		t.Errorf("expected global name my-workload, got %q", spec.GlobalConfig.Name)
	}
}
