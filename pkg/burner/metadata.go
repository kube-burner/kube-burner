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
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/version"
)

type jobSummary struct {
	Timestamp   time.Time              `json:"timestamp"`
	UUID        string                 `json:"uuid"`
	MetricName  string                 `json:"metricName"`
	ElapsedTime float64                `json:"elapsedTime"`
	JobConfig   config.Job             `json:"jobConfig"`
	Metadata    map[string]interface{} `json:"metadata"`
	Version     string                 `json:"version"`
}

const jobSummaryMetric = "jobSummary"

// indexMetadataInfo Generates and indexes a document with metadata information of the passed job
func indexjobSummaryInfo(indexer *indexers.Indexer, uuid string, elapsedTime float64, jobConfig config.Job, timestamp time.Time, metadata map[string]interface{}) {
	metadataInfo := []interface{}{
		jobSummary{
			UUID:        uuid,
			ElapsedTime: elapsedTime,
			JobConfig:   jobConfig,
			MetricName:  jobSummaryMetric,
			Timestamp:   timestamp,
			Metadata:    metadata,
			Version:     fmt.Sprintf("%v@%v", version.Version, version.GitCommit),
		},
	}
	(*indexer).Index(metadataInfo, indexers.IndexingOpts{MetricName: jobSummaryMetric, JobName: jobConfig.Name})
}
