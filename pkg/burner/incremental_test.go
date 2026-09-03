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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/prometheus"
	"github.com/kube-burner/kube-burner/v2/pkg/util/metrics"
)

func newTestExecutor(name string, uuid string) JobExecutor {
	return JobExecutor{
		Job: config.Job{
			Name:            name,
			IncrementalLoad: &config.IncrementalLoad{},
		},
		uuid: uuid,
	}
}

// TestScrapeStepMetricsDisabled verifies no scraping or indexing when disabled
func TestScrapeStepMetricsDisabled(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)

	mockIdx := &mockIndexer{}
	scraper := metrics.Scraper{
		IndexerList: map[string]indexers.Indexer{"mock": mockIdx},
	}

	start := time.Now().UTC()
	job := &prometheus.Job{
		Start:     start,
		End:       start.Add(10 * time.Second),
		JobConfig: ex.Job,
		UUID:      testUUID,
	}

	scrapeStepMetrics(&ex, job, map[string]any{}, scraper, 100)

	if len(mockIdx.indexed) != 0 {
		t.Errorf("expected no indexing when scraping disabled, got %d indexes", len(mockIdx.indexed))
	}
}

// TestScrapeStepMetricsIndexesJobSummary verifies indexing when enabled with no prometheus
func TestScrapeStepMetricsIndexesJobSummary(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)
	ex.IncrementalLoad.ScrapeMetricsPerStep = true

	mockIdx := &mockIndexer{}
	scraper := metrics.Scraper{
		IndexerList: map[string]indexers.Indexer{"mock": mockIdx},
	}

	start := time.Now().UTC()
	end := start.Add(15 * time.Second)
	stepUUID := "step-abc"

	job := &prometheus.Job{
		Start:               start,
		End:                 end,
		JobConfig:           ex.Job,
		UUID:                testUUID,
		IncrementalLoadUUID: stepUUID,
	}

	scrapeStepMetrics(&ex, job, map[string]any{"custom": "metadata"}, scraper, 150)

	if len(mockIdx.indexed) != 1 {
		t.Fatalf("expected 1 index call, got %d", len(mockIdx.indexed))
	}
	if mockIdx.indexed[0].MetricName != jobSummaryMetric {
		t.Errorf("expected metricName %q, got %q", jobSummaryMetric, mockIdx.indexed[0].MetricName)
	}

	if len(mockIdx.docs[0]) != 1 {
		t.Fatalf("expected 1 document indexed, got %d", len(mockIdx.docs[0]))
	}

	doc := mockIdx.docs[0][0].(map[string]any)
	if doc["uuid"] != testUUID {
		t.Errorf("expected uuid %q, got %v", testUUID, doc["uuid"])
	}
	if doc["incrementalLoadUUID"] != stepUUID {
		t.Errorf("expected incrementalLoadUUID %q, got %v", stepUUID, doc["incrementalLoadUUID"])
	}
	if doc["custom"] != "metadata" {
		t.Errorf("expected custom metadata in doc, got %v", doc["custom"])
	}
	if doc["passed"] != true {
		t.Errorf("expected passed=true, got %v", doc["passed"])
	}
	if doc["elapsedTime"] != 15.0 {
		t.Errorf("expected elapsedTime=15, got %v", doc["elapsedTime"])
	}
}

// TestScrapeStepMetricsSkipIndexing verifies no indexing when SkipIndexing=true
func TestScrapeStepMetricsSkipIndexing(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)
	ex.IncrementalLoad.ScrapeMetricsPerStep = true
	ex.SkipIndexing = true

	mockIdx := &mockIndexer{}
	scraper := metrics.Scraper{
		IndexerList: map[string]indexers.Indexer{"mock": mockIdx},
	}

	start := time.Now().UTC()
	job := &prometheus.Job{
		Start:     start,
		End:       start.Add(5 * time.Second),
		JobConfig: ex.Job,
		UUID:      testUUID,
	}

	scrapeStepMetrics(&ex, job, map[string]any{}, scraper, 50)

	if len(mockIdx.indexed) != 0 {
		t.Errorf("expected no indexing with SkipIndexing=true, got %d indexes", len(mockIdx.indexed))
	}
}

// TestScrapeStepMetricsNoPrometheusClients verifies MetricsScraped stays false with no clients
func TestScrapeStepMetricsNoPrometheusClients(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)
	ex.IncrementalLoad.ScrapeMetricsPerStep = true

	mockIdx := &mockIndexer{}
	scraper := metrics.Scraper{
		IndexerList: map[string]indexers.Indexer{"mock": mockIdx},
	}

	start := time.Now().UTC()
	job := &prometheus.Job{
		Start:     start,
		End:       start.Add(7 * time.Second),
		JobConfig: ex.Job,
		UUID:      testUUID,
	}

	scrapeStepMetrics(&ex, job, map[string]any{}, scraper, 70)

	if job.MetricsScraped {
		t.Error("expected job.MetricsScraped=false when no prometheus clients")
	}

	// Indexing should still happen
	if len(mockIdx.indexed) != 1 {
		t.Errorf("expected indexing even without prometheus, got %d", len(mockIdx.indexed))
	}
}

func TestRunIncrementalCreateJobPreservesPerStepScrapeState(t *testing.T) {
	ex := newTestExecutor(randomJobName(), randomUUID())
	ex.IncrementalLoad = &config.IncrementalLoad{
		StartIterations:      1,
		TotalIterations:      1,
		ScrapeMetricsPerStep: true,
		Pattern: config.LoadPattern{
			Type:   config.LinearPattern,
			Linear: &config.LinearLoadConfig{StepSize: 1},
		},
	}
	provider := newTestKubeClientProvider(t)
	ex.clientSet, _ = provider.DefaultClientSet()
	ex.createdNamespaces = make(map[string]bool)
	mockIdx := &mockIndexer{}
	scraper := metrics.Scraper{
		IndexerList:       map[string]indexers.Indexer{"mock": mockIdx},
		PrometheusClients: []*prometheus.Prometheus{{}},
	}

	configSpec := config.Spec{GlobalConfig: config.GlobalConfig{GCMetrics: true}}
	_, jobs := ex.RunIncrementalCreateJob(context.Background(), NewIterationCalculator(ex), nil, provider, nil, ex.Name, scraper, configSpec)
	indexMetrics(ex.uuid, jobs, map[string]returnPair{}, scraper, config.Spec{}, true, "", false)

	if len(jobs) != 2 {
		t.Fatalf("expected incremental work and GC jobs, got %d", len(jobs))
	}
	for _, job := range jobs {
		if !job.MetricsScraped {
			t.Errorf("%s job was appended before its scrape result was recorded", job.JobConfig.Name)
		}
	}

	var summaryCount int
	for _, docs := range mockIdx.docs {
		summaryCount += len(docs)
	}
	if summaryCount != 2 {
		t.Errorf("expected only the two per-step job summaries, got %d", summaryCount)
	}
}

func newTestKubeClientProvider(t *testing.T) *config.KubeClientProvider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"gitVersion":"v1.30.0"}`))
		case "/healthz":
			_, _ = w.Write([]byte("ok"))
		case "/api/v1/nodes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"NodeList","items":[]}`))
		case "/api/v1/namespaces":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"NamespaceList","items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	kubeconfig := fmt.Sprintf("apiVersion: v1\nclusters:\n- cluster:\n    server: %s\n    insecure-skip-tls-verify: true\n  name: test\ncontexts:\n- context:\n    cluster: test\n    user: test\n  name: test\ncurrent-context: test\nkind: Config\nusers:\n- name: test\n  user: {}\n", server.URL)
	configPath := t.TempDir() + "/kubeconfig"
	if err := os.WriteFile(configPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return config.NewKubeClientProvider(configPath, "")
}

// TestIncrementalStepJobsEquivalence verifies refactored step job creation matches original
func TestIncrementalStepJobsEquivalence(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)

	stepStart := time.Now().UTC()
	stepEnd := stepStart.Add(30 * time.Second)
	stepRunID := "step-run-123"

	// New: using newScrapeJob with options (as in current code)
	workJob := ex.newScrapeJob(stepStart, WithEnd(stepEnd), WithIncrementalUUID(stepRunID))

	// Old: manual construction (as in code before this branch)
	oldJob := prometheus.Job{
		Start:               stepStart,
		End:                 stepEnd,
		JobConfig:           ex.Job,
		UUID:                testUUID,
		IncrementalLoadUUID: stepRunID,
	}

	assertJobsEqual(t, "work", workJob, oldJob)
}

// TestIncrementalGCJobsEquivalence verifies refactored GC job creation matches original
func TestIncrementalGCJobsEquivalence(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)

	gcStart := time.Now().UTC()
	gcEnd := gcStart.Add(5 * time.Second)
	stepRunID := "step-run-456"

	// New: using newScrapeJob with options
	gcJob := ex.newScrapeJob(gcStart, WithEnd(gcEnd), WithIncrementalUUID(stepRunID), WithGCJob())

	// Old: manual construction
	oldGCJob := prometheus.Job{
		Start: gcStart,
		End:   gcEnd,
		JobConfig: config.Job{
			Name: garbageCollectionJob,
		},
		UUID:                testUUID,
		IncrementalLoadUUID: stepRunID,
	}

	assertJobsEqual(t, "gc", gcJob, oldGCJob)
}

// TestIncrementalStepJobsAppendOrder verifies append order matches original pattern
func TestIncrementalStepJobsAppendOrder(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := newTestExecutor(jobName, testUUID)

	now := time.Now().UTC()
	var stepJobs []prometheus.Job

	// Simulate two incremental steps with GC
	for i := range 2 {
		stepStart := now.Add(time.Duration(i*60) * time.Second)
		stepEnd := stepStart.Add(30 * time.Second)
		stepRunID := "step-" + string(rune('a'+i))

		workJob := ex.newScrapeJob(stepStart, WithEnd(stepEnd), WithIncrementalUUID(stepRunID))
		stepJobs = append(stepJobs, workJob)

		gcStart := stepEnd
		gcEnd := gcStart.Add(5 * time.Second)
		gcJob := ex.newScrapeJob(gcStart, WithEnd(gcEnd), WithIncrementalUUID(stepRunID), WithGCJob())
		stepJobs = append(stepJobs, gcJob)
	}

	if len(stepJobs) != 4 {
		t.Fatalf("expected 4 step jobs (2 work + 2 gc), got %d", len(stepJobs))
	}

	// Verify ordering: work, gc, work, gc
	if stepJobs[0].JobConfig.Name != jobName {
		t.Errorf("job[0] should be work job, got %q", stepJobs[0].JobConfig.Name)
	}
	if stepJobs[1].JobConfig.Name != garbageCollectionJob {
		t.Errorf("job[1] should be gc job, got %q", stepJobs[1].JobConfig.Name)
	}
	if stepJobs[2].JobConfig.Name != jobName {
		t.Errorf("job[2] should be work job, got %q", stepJobs[2].JobConfig.Name)
	}
	if stepJobs[3].JobConfig.Name != garbageCollectionJob {
		t.Errorf("job[3] should be gc job, got %q", stepJobs[3].JobConfig.Name)
	}

	// All should share UUID
	for i, j := range stepJobs {
		if j.UUID != testUUID {
			t.Errorf("job[%d] UUID mismatch: expected %q, got %q", i, testUUID, j.UUID)
		}
	}
}

func assertJobsEqual(t *testing.T, label string, got, want prometheus.Job) {
	t.Helper()
	if got.Start != want.Start {
		t.Errorf("%s: Start mismatch: got=%v want=%v", label, got.Start, want.Start)
	}
	if got.End != want.End {
		t.Errorf("%s: End mismatch: got=%v want=%v", label, got.End, want.End)
	}
	if got.UUID != want.UUID {
		t.Errorf("%s: UUID mismatch: got=%q want=%q", label, got.UUID, want.UUID)
	}
	if got.IncrementalLoadUUID != want.IncrementalLoadUUID {
		t.Errorf("%s: IncrementalLoadUUID mismatch: got=%q want=%q", label, got.IncrementalLoadUUID, want.IncrementalLoadUUID)
	}
	if got.JobConfig.Name != want.JobConfig.Name {
		t.Errorf("%s: JobConfig.Name mismatch: got=%q want=%q", label, got.JobConfig.Name, want.JobConfig.Name)
	}
}
