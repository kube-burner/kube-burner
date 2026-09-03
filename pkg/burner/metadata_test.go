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
	"testing"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/prometheus"
	"github.com/kube-burner/kube-burner/v2/pkg/util/metrics"
)

const testUuid = "test-uuid"

type mockIndexer struct {
	indexed []indexers.IndexingOpts
	docs    [][]interface{}
}

func (m *mockIndexer) Index(docs []interface{}, opts indexers.IndexingOpts) (string, error) {
	m.indexed = append(m.indexed, opts)
	m.docs = append(m.docs, docs)
	return "indexed", nil
}

func TestIndexJobSummary(t *testing.T) {
	now := time.Now().UTC()
	summaries := []JobSummary{
		{
			UUID:                testUuid,
			IncrementalLoadUUID: "step-uuid-1",
			Timestamp:           now,
			EndTimestamp:        now.Add(30 * time.Second),
			ElapsedTime:         30,
			JobConfig:           config.Job{Name: "test-job"},
			Metadata:            map[string]any{"incrementalLoadUUID": "step-uuid-1", "platform": "test"},
			Passed:              true,
			Version:             "v1.0@abc123",
			MetricName:          jobSummaryMetric,
		},
	}

	mock := &mockIndexer{}
	IndexJobSummary(summaries, mock)

	if len(mock.indexed) != 1 {
		t.Fatalf("expected 1 index call, got %d", len(mock.indexed))
	}
	if mock.indexed[0].MetricName != jobSummaryMetric {
		t.Errorf("expected metricName %q, got %q", jobSummaryMetric, mock.indexed[0].MetricName)
	}
	if len(mock.docs[0]) != 1 {
		t.Fatalf("expected 1 document, got %d", len(mock.docs[0]))
	}

	doc := mock.docs[0][0].(map[string]any)
	if doc["uuid"] != testUuid {
		t.Errorf("expected uuid %q, got %v", testUuid, doc["uuid"])
	}
	if doc["incrementalLoadUUID"] != "step-uuid-1" {
		t.Errorf("expected incrementalLoadUUID %q, got %v", "step-uuid-1", doc["incrementalLoadUUID"])
	}
	if doc["metricName"] != jobSummaryMetric {
		t.Errorf("expected metricName %q, got %v", jobSummaryMetric, doc["metricName"])
	}
	if doc["passed"] != true {
		t.Errorf("expected passed=true, got %v", doc["passed"])
	}
	if doc["platform"] != "test" {
		t.Errorf("expected metadata platform=test, got %v", doc["platform"])
	}
	if doc["version"] != "v1.0@abc123" {
		t.Errorf("expected version %q, got %v", "v1.0@abc123", doc["version"])
	}
}

func TestIndexJobSummaryMultipleSteps(t *testing.T) {
	now := time.Now().UTC()
	summaries := []JobSummary{
		{
			UUID:                testUuid,
			IncrementalLoadUUID: "step-1",
			Timestamp:           now,
			EndTimestamp:        now.Add(10 * time.Second),
			ElapsedTime:         10,
			JobConfig:           config.Job{Name: "test-job"},
			Metadata:            map[string]any{"incrementalLoadUUID": "step-1"},
			Passed:              true,
			MetricName:          jobSummaryMetric,
		},
		{
			UUID:                testUuid,
			IncrementalLoadUUID: "step-2",
			Timestamp:           now.Add(10 * time.Second),
			EndTimestamp:        now.Add(25 * time.Second),
			ElapsedTime:         15,
			JobConfig:           config.Job{Name: "test-job"},
			Metadata:            map[string]any{"incrementalLoadUUID": "step-2"},
			Passed:              true,
			MetricName:          jobSummaryMetric,
		},
	}

	mock := &mockIndexer{}
	IndexJobSummary(summaries, mock)

	if len(mock.docs[0]) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(mock.docs[0]))
	}

	doc1 := mock.docs[0][0].(map[string]any)
	doc2 := mock.docs[0][1].(map[string]any)

	if doc1["incrementalLoadUUID"] != "step-1" {
		t.Errorf("doc1: expected incrementalLoadUUID step-1, got %v", doc1["incrementalLoadUUID"])
	}
	if doc2["incrementalLoadUUID"] != "step-2" {
		t.Errorf("doc2: expected incrementalLoadUUID step-2, got %v", doc2["incrementalLoadUUID"])
	}
	if doc1["uuid"] != testUuid || doc2["uuid"] != testUuid {
		t.Error("both docs should share same uuid")
	}
}

func TestIndexJobSummaryMetadataMerged(t *testing.T) {
	summaries := []JobSummary{
		{
			UUID:       "uuid-1",
			Timestamp:  time.Now().UTC(),
			JobConfig:  config.Job{Name: "job1"},
			Metadata:   map[string]any{"cluster": "perf-cluster", "incrementalLoadUUID": "step-x"},
			Passed:     true,
			MetricName: jobSummaryMetric,
		},
	}

	mock := &mockIndexer{}
	IndexJobSummary(summaries, mock)

	doc := mock.docs[0][0].(map[string]any)

	// Metadata fields merged into top-level doc
	if doc["cluster"] != "perf-cluster" {
		t.Errorf("expected metadata field cluster=perf-cluster, got %v", doc["cluster"])
	}
	// incrementalLoadUUID from Metadata overwrites the JSON-marshaled field
	if doc["incrementalLoadUUID"] != "step-x" {
		t.Errorf("expected incrementalLoadUUID from metadata, got %v", doc["incrementalLoadUUID"])
	}
}

func TestIndexMetricsSkipsAlreadyScrapedJobSummary(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockIndexer{}

	executedJobs := []prometheus.Job{
		{
			Start:               now,
			End:                 now.Add(10 * time.Second),
			JobConfig:           config.Job{Name: "job1"},
			UUID:                "uuid-1",
			IncrementalLoadUUID: "step-1",
			MetricsScraped:      true,
		},
		{
			Start:               now.Add(10 * time.Second),
			End:                 now.Add(20 * time.Second),
			JobConfig:           config.Job{Name: "job1"},
			UUID:                "uuid-1",
			IncrementalLoadUUID: "step-2",
			MetricsScraped:      false,
		},
		{
			Start:     now,
			End:       now.Add(20 * time.Second),
			JobConfig: config.Job{Name: "job1"},
			UUID:      "uuid-1",
		},
	}

	scraper := metrics.Scraper{
		IndexerList:     map[string]indexers.Indexer{"mock": mock},
		SummaryMetadata: map[string]any{"platform": "test"},
	}

	indexMetrics("uuid-1", executedJobs, map[string]returnPair{}, scraper, config.Spec{}, true, "", false)

	if len(mock.indexed) != 1 {
		t.Fatalf("expected 1 IndexJobSummary call, got %d", len(mock.indexed))
	}

	docs := mock.docs[0]
	if len(docs) != 2 {
		t.Fatalf("expected 2 job summaries (skipping MetricsScraped=true), got %d", len(docs))
	}

	doc1 := docs[0].(map[string]any)
	doc2 := docs[1].(map[string]any)

	if doc1["incrementalLoadUUID"] != "step-2" {
		t.Errorf("first summary should be step-2 (unscraped), got %v", doc1["incrementalLoadUUID"])
	}
	if _, has := doc2["incrementalLoadUUID"]; has && doc2["incrementalLoadUUID"] != "" {
		t.Errorf("second summary should be the parent job with no incrementalLoadUUID, got %v", doc2["incrementalLoadUUID"])
	}
}

func TestIndexMetricsSkipsScrapedGCJobSummary(t *testing.T) {
	now := time.Now().UTC()
	mock := &mockIndexer{}

	executedJobs := []prometheus.Job{
		{
			Start:               now,
			End:                 now.Add(10 * time.Second),
			JobConfig:           config.Job{Name: "job1"},
			UUID:                "uuid-1",
			IncrementalLoadUUID: "step-1",
			MetricsScraped:      true,
		},
		{
			Start:               now.Add(10 * time.Second),
			End:                 now.Add(15 * time.Second),
			JobConfig:           config.Job{Name: garbageCollectionJob},
			UUID:                "uuid-1",
			IncrementalLoadUUID: "step-1",
			MetricsScraped:      true,
		},
		{
			Start:     now,
			End:       now.Add(15 * time.Second),
			JobConfig: config.Job{Name: "job1"},
			UUID:      "uuid-1",
		},
	}

	scraper := metrics.Scraper{
		IndexerList:     map[string]indexers.Indexer{"mock": mock},
		SummaryMetadata: map[string]any{"platform": "test"},
	}

	indexMetrics("uuid-1", executedJobs, map[string]returnPair{}, scraper, config.Spec{}, true, "", false)

	docs := mock.docs[0]
	if len(docs) != 1 {
		t.Fatalf("expected 1 job summary (work+gc scraped, only parent remains), got %d", len(docs))
	}

	doc := docs[0].(map[string]any)
	if doc["jobConfig"].(map[string]any)["name"] != "job1" {
		t.Errorf("expected parent job summary for job1, got %v", doc["jobConfig"])
	}
}
