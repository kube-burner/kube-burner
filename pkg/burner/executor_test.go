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

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/prometheus"
)

// TestNewScrapeJobBasic verifies basic job creation with defaults
func TestNewScrapeJobBasic(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job: config.Job{
			Name:      jobName,
			Namespace: "test-ns",
		},
		uuid: testUUID,
	}

	start := time.Now().UTC()
	job := ex.newScrapeJob(start)

	if job.Start != start {
		t.Errorf("expected start=%v, got %v", start, job.Start)
	}
	if job.JobConfig.Name != jobName {
		t.Errorf("expected jobConfig.Name=%q, got %q", jobName, job.JobConfig.Name)
	}
	if job.UUID != testUUID {
		t.Errorf("expected UUID=%q, got %q", testUUID, job.UUID)
	}
	if !job.End.IsZero() {
		t.Errorf("expected End to be zero, got %v", job.End)
	}
	if job.IncrementalLoadUUID != "" {
		t.Errorf("expected IncrementalLoadUUID empty, got %q", job.IncrementalLoadUUID)
	}
}

// TestNewScrapeJobWithEnd verifies WithEnd option
func TestNewScrapeJobWithEnd(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job:  config.Job{Name: jobName},
		uuid: testUUID,
	}

	start := time.Now().UTC()
	end := start.Add(30 * time.Second)
	job := ex.newScrapeJob(start, WithEnd(end))

	if job.Start != start {
		t.Errorf("expected start=%v, got %v", start, job.Start)
	}
	if job.End != end {
		t.Errorf("expected end=%v, got %v", end, job.End)
	}
	if job.UUID != testUUID {
		t.Errorf("expected UUID=%q, got %q", testUUID, job.UUID)
	}
}

// TestNewScrapeJobWithIncrementalUUID verifies WithIncrementalUUID option
func TestNewScrapeJobWithIncrementalUUID(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job:  config.Job{Name: jobName},
		uuid: testUUID,
	}

	stepUUID := "step-12345"
	start := time.Now().UTC()
	end := start.Add(15 * time.Second)

	job := ex.newScrapeJob(start, WithEnd(end), WithIncrementalUUID(stepUUID))

	if job.IncrementalLoadUUID != stepUUID {
		t.Errorf("expected IncrementalLoadUUID=%q, got %q", stepUUID, job.IncrementalLoadUUID)
	}
	if job.UUID != testUUID {
		t.Errorf("expected UUID=%q, got %q", testUUID, job.UUID)
	}
	if job.End != end {
		t.Errorf("expected end=%v, got %v", end, job.End)
	}
}

// TestNewScrapeJobWithGCJob verifies WithGCJob option
func TestNewScrapeJobWithGCJob(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job:  config.Job{Name: jobName},
		uuid: testUUID,
	}

	start := time.Now().UTC()
	end := start.Add(5 * time.Second)

	job := ex.newScrapeJob(start, WithEnd(end), WithGCJob())

	if job.JobConfig.Name != garbageCollectionJob {
		t.Errorf("expected JobConfig.Name=%q, got %q", garbageCollectionJob, job.JobConfig.Name)
	}
	if job.UUID != testUUID {
		t.Errorf("expected UUID=%q (from executor), got %q", testUUID, job.UUID)
	}
}

// TestNewScrapeJobAllOptions verifies combining all options
func TestNewScrapeJobAllOptions(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job:  config.Job{Name: jobName},
		uuid: testUUID,
	}

	stepUUID := "gc-step-789"
	start := time.Now().UTC()
	end := start.Add(10 * time.Second)

	job := ex.newScrapeJob(start, WithEnd(end), WithIncrementalUUID(stepUUID), WithGCJob())

	if job.Start != start {
		t.Errorf("expected start=%v, got %v", start, job.Start)
	}
	if job.End != end {
		t.Errorf("expected end=%v, got %v", end, job.End)
	}
	if job.IncrementalLoadUUID != stepUUID {
		t.Errorf("expected IncrementalLoadUUID=%q, got %q", stepUUID, job.IncrementalLoadUUID)
	}
	if job.JobConfig.Name != garbageCollectionJob {
		t.Errorf("expected JobConfig.Name=%q, got %q", garbageCollectionJob, job.JobConfig.Name)
	}
	if job.UUID != testUUID {
		t.Errorf("expected UUID=%q, got %q", testUUID, job.UUID)
	}
}

// TestNewScrapeJobMultipleInvocations verifies independent job creation
func TestNewScrapeJobMultipleInvocations(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job:  config.Job{Name: jobName},
		uuid: testUUID,
	}

	start1 := time.Now().UTC()
	start2 := start1.Add(20 * time.Second)
	end1 := start1.Add(10 * time.Second)

	job1 := ex.newScrapeJob(start1, WithEnd(end1), WithIncrementalUUID("step-1"))
	job2 := ex.newScrapeJob(start2, WithGCJob())

	// Verify job1 unchanged after creating job2
	if job1.Start != start1 {
		t.Errorf("job1 start changed, expected %v, got %v", start1, job1.Start)
	}
	if job1.End != end1 {
		t.Errorf("job1 end changed, expected %v, got %v", end1, job1.End)
	}
	if job1.IncrementalLoadUUID != "step-1" {
		t.Errorf("job1 incrementalUUID changed, got %q", job1.IncrementalLoadUUID)
	}
	if job1.JobConfig.Name != jobName {
		t.Errorf("job1 jobConfig changed, got %q", job1.JobConfig.Name)
	}

	// Verify job2 has correct values
	if job2.Start != start2 {
		t.Errorf("job2 start wrong, expected %v, got %v", start2, job2.Start)
	}
	if !job2.End.IsZero() {
		t.Errorf("job2 end should be zero, got %v", job2.End)
	}
	if job2.JobConfig.Name != garbageCollectionJob {
		t.Errorf("job2 should be GC job, got %q", job2.JobConfig.Name)
	}
	if job2.IncrementalLoadUUID != "" {
		t.Errorf("job2 incrementalUUID should be empty, got %q", job2.IncrementalLoadUUID)
	}
}

// TestJobCreationEquivalence verifies refactored code produces same jobs as original
func TestJobCreationEquivalence(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job: config.Job{
			Name:      jobName,
			Namespace: "perf-ns",
		},
		uuid: testUUID,
	}

	start := time.Now().UTC()
	end := start.Add(25 * time.Second)
	stepUUID := "step-xyz"

	// New refactored approach
	newJob := ex.newScrapeJob(start, WithEnd(end), WithIncrementalUUID(stepUUID))

	// Equivalent to old approach (manual construction)
	oldJob := prometheus.Job{
		Start:               start,
		End:                 end,
		JobConfig:           ex.Job,
		UUID:                testUUID,
		IncrementalLoadUUID: stepUUID,
	}

	// Verify all fields match
	if newJob.Start != oldJob.Start {
		t.Errorf("start mismatch: new=%v old=%v", newJob.Start, oldJob.Start)
	}
	if newJob.End != oldJob.End {
		t.Errorf("end mismatch: new=%v old=%v", newJob.End, oldJob.End)
	}
	if newJob.UUID != oldJob.UUID {
		t.Errorf("UUID mismatch: new=%q old=%q", newJob.UUID, oldJob.UUID)
	}
	if newJob.IncrementalLoadUUID != oldJob.IncrementalLoadUUID {
		t.Errorf("IncrementalLoadUUID mismatch: new=%q old=%q", newJob.IncrementalLoadUUID, oldJob.IncrementalLoadUUID)
	}
	if newJob.JobConfig.Name != oldJob.JobConfig.Name {
		t.Errorf("JobConfig.Name mismatch: new=%q old=%q", newJob.JobConfig.Name, oldJob.JobConfig.Name)
	}
	if newJob.JobConfig.Namespace != oldJob.JobConfig.Namespace {
		t.Errorf("JobConfig.Namespace mismatch: new=%q old=%q", newJob.JobConfig.Namespace, oldJob.JobConfig.Namespace)
	}
}

// TestGCJobCreationEquivalence verifies GC job creation matches original
func TestGCJobCreationEquivalence(t *testing.T) {
	jobName := randomJobName()
	testUUID := randomUUID()
	ex := &JobExecutor{
		Job:  config.Job{Name: jobName},
		uuid: testUUID,
	}

	gcStart := time.Now().UTC()
	gcEnd := gcStart.Add(3 * time.Second)
	stepUUID := "step-456"

	// New refactored approach
	newGCJob := ex.newScrapeJob(gcStart, WithEnd(gcEnd), WithIncrementalUUID(stepUUID), WithGCJob())

	// Equivalent to old approach
	oldGCJob := prometheus.Job{
		Start: gcStart,
		End:   gcEnd,
		JobConfig: config.Job{
			Name: garbageCollectionJob,
		},
		UUID:                testUUID,
		IncrementalLoadUUID: stepUUID,
	}

	if newGCJob.Start != oldGCJob.Start {
		t.Errorf("GC start mismatch: new=%v old=%v", newGCJob.Start, oldGCJob.Start)
	}
	if newGCJob.End != oldGCJob.End {
		t.Errorf("GC end mismatch: new=%v old=%v", newGCJob.End, oldGCJob.End)
	}
	if newGCJob.UUID != oldGCJob.UUID {
		t.Errorf("GC UUID mismatch: new=%q old=%q", newGCJob.UUID, oldGCJob.UUID)
	}
	if newGCJob.IncrementalLoadUUID != oldGCJob.IncrementalLoadUUID {
		t.Errorf("GC IncrementalLoadUUID mismatch: new=%q old=%q", newGCJob.IncrementalLoadUUID, oldGCJob.IncrementalLoadUUID)
	}
	if newGCJob.JobConfig.Name != garbageCollectionJob {
		t.Errorf("expected GC job name, got %q", newGCJob.JobConfig.Name)
	}
}
