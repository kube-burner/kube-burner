// Copyright 2020 The Kube-burner Authors.
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
	"time"

	"maps"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	log "github.com/sirupsen/logrus"
)

type JobSummary struct {
	Timestamp           time.Time      `json:"timestamp"`
	EndTimestamp        time.Time      `json:"endTimestamp"`
	ChurnStartTimestamp *time.Time     `json:"churnStartTimestamp,omitempty"`
	ChurnEndTimestamp   *time.Time     `json:"churnEndTimestamp,omitempty"`
	ElapsedTime         float64        `json:"elapsedTime"`
	UUID                string         `json:"uuid"`
	MetricName          string         `json:"metricName"`
	JobConfig           config.Job     `json:"jobConfig"`
	Version             string         `json:"version,omitempty"`
	Passed              bool           `json:"passed"`
	ExecutionErrors     string         `json:"executionErrors,omitempty"`
	Metadata            map[string]any `json:"-"`
}

const jobSummaryMetric = "jobSummary"

// IndexJobSummary indexes jobSummaries Generates and indexes a document with metadata information of the passed job
func IndexJobSummary(jobSummaries []JobSummary, indexer indexers.Indexer) {
	log.Info("Indexing job summaries")
	var jobSummariesInt []any
	for _, summary := range jobSummaries {
		summaryMap := make(map[string]any)
		j, _ := json.Marshal(summary)
		json.Unmarshal(j, &summaryMap)
		summaryMap["metricName"] = jobSummaryMetric
		maps.Copy(summaryMap, summary.Metadata)
		jobSummariesInt = append(jobSummariesInt, summaryMap)
	}
	indexingOpts := indexers.IndexingOpts{
		MetricName: jobSummaryMetric,
	}
	resp, err := indexer.Index(jobSummariesInt, indexingOpts)
	if err != nil {
		log.Error(err)
	} else {
		log.Info(resp)
	}
}
