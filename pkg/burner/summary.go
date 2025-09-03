// Copyright 2024 The Kube-burner Authors.
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
	"time"

	"github.com/kube-burner/kube-burner/pkg/util"
)

// BenchmarkSummary represents the overall benchmark summary
type BenchmarkSummary struct {
	UUID            string         `json:"uuid" yaml:"uuid"`
	Version         string         `json:"version" yaml:"version"`
	StartTime       time.Time      `json:"startTime" yaml:"startTime"`
	EndTime         time.Time      `json:"endTime" yaml:"endTime"`
	ElapsedTime     float64        `json:"elapsedTime" yaml:"elapsedTime"`
	TotalJobs       int            `json:"totalJobs" yaml:"totalJobs"`
	PassedJobs      int            `json:"passedJobs" yaml:"passedJobs"`
	FailedJobs      int            `json:"failedJobs" yaml:"failedJobs"`
	ReturnCode      int            `json:"returnCode" yaml:"returnCode"`
	ExecutionErrors string         `json:"executionErrors,omitempty" yaml:"executionErrors,omitempty"`
	Jobs            []JobSummary   `json:"jobs" yaml:"jobs"`
	Metadata        map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// MeasurementSummary represents a summary of measurements
type MeasurementSummary struct {
	Name        string         `json:"name" yaml:"name"`
	JobName     string         `json:"jobName" yaml:"jobName"`
	UUID        string         `json:"uuid" yaml:"uuid"`
	Timestamp   time.Time      `json:"timestamp" yaml:"timestamp"`
	MetricCount int            `json:"metricCount" yaml:"metricCount"`
	Metadata    map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// OutputSummary writes the benchmark summary to stdout in the specified format
func OutputSummary(summary BenchmarkSummary, format util.OutputFormat) error {
	return util.WriteOutput(summary, format)
}

// OutputSummaryToFile writes the benchmark summary to a file in the specified format
func OutputSummaryToFile(summary BenchmarkSummary, filename string, format util.OutputFormat) error {
	return util.WriteOutputToFile(summary, filename, format)
}

// OutputMeasurements writes measurement summaries to stdout in the specified format
func OutputMeasurements(measurements []MeasurementSummary, format util.OutputFormat) error {
	return util.WriteOutput(measurements, format)
}

// OutputMeasurementsToFile writes measurement summaries to a file in the specified format
func OutputMeasurementsToFile(measurements []MeasurementSummary, filename string, format util.OutputFormat) error {
	return util.WriteOutputToFile(measurements, filename, format)
}
