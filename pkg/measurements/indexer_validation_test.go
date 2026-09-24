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

package measurements

import (
	"strings"
	"testing"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
)

func TestValidateIndexers(t *testing.T) {
	configSpec := config.Spec{
		MetricsEndpoints: []config.MetricsEndpoint{
			{
				IndexerConfig: indexers.IndexerConfig{Type: indexers.LocalIndexer},
				Alias:         "local-indexer",
			},
			{
				IndexerConfig: indexers.IndexerConfig{Type: indexers.OpenSearchIndexer},
				Alias:         "os-indexer",
			},
			{Alias: "alerts-only"},
			{IndexerConfig: indexers.IndexerConfig{Type: indexers.LocalIndexer}},
		},
	}

	tests := []struct {
		name        string
		measurement types.Measurement
		wantErr     bool
		errContains string
	}{
		{
			name:        "no indexers specified",
			measurement: types.Measurement{Name: "podLatency"},
		},
		{
			name: "both indexers valid",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "local-indexer",
				QuantilesIndexer:  "os-indexer",
			},
		},
		{
			name: "only timeseriesIndexer, valid",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "local-indexer",
			},
		},
		{
			name: "generated indexer alias is valid",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "indexer-3",
			},
		},
		{
			name: "alias without an indexer is invalid",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "alerts-only",
			},
			wantErr:     true,
			errContains: "timeseriesIndexer",
		},
		{
			name: "unknown timeseriesIndexer while quantilesIndexer is valid",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "does-not-exist",
				QuantilesIndexer:  "os-indexer",
			},
			wantErr:     true,
			errContains: "timeseriesIndexer",
		},
		{
			name: "unknown quantilesIndexer while timeseriesIndexer is valid",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "local-indexer",
				QuantilesIndexer:  "does-not-exist",
			},
			wantErr:     true,
			errContains: "quantilesIndexer",
		},
		{
			name: "both indexers unknown",
			measurement: types.Measurement{
				Name:              "podLatency",
				TimeseriesIndexer: "nope-1",
				QuantilesIndexer:  "nope-2",
			},
			wantErr:     true,
			errContains: "timeseriesIndexer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIndexers(configSpec, tt.measurement)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not mention %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
