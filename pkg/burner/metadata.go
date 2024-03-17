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
	"fmt"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/go-commons/version"
	"github.com/kube-burner/kube-burner/pkg/config"
	log "github.com/sirupsen/logrus"
)

type timings struct {
	Timestamp           time.Time  `json:"timestamp"`
	EndTimestamp        time.Time  `json:"endTimestamp"`
	ChurnStartTimestamp *time.Time `json:"churnStartTimestamp,omitempty"`
	ChurnEndTimestamp   *time.Time `json:"churnEndTimestamp,omitempty"`
	ElapsedTime         float64    `json:"elapsedTime"`
}

type jobSummary struct {
	timings
	UUID       string                 `json:"uuid"`
	MetricName string                 `json:"metricName"`
	JobConfig  config.Job             `json:"jobConfig"`
	Metadata   map[string]interface{} `json:"metadata"`
	Version    string                 `json:"version"`
}

const jobSummaryMetric = "jobSummary"

// indexMetadataInfo Generates and indexes a document with metadata information of the passed job
func indexjobSummaryInfo(indexer indexers.Indexer, uuid string, jobTimings timings, jobConfig config.Job, metadata map[string]interface{}) {
	metadataInfo := []interface{}{
		jobSummary{
			UUID:       uuid,
			JobConfig:  jobConfig,
			MetricName: jobSummaryMetric,
			Metadata:   metadata,
			Version:    fmt.Sprintf("%v@%v", version.Version, version.GitCommit),
			timings:    jobTimings,
		},
	}
	log.Infof("Indexing metric %s", jobSummaryMetric)
	indexingOpts := indexers.IndexingOpts{
		MetricName: fmt.Sprintf("%s-%s", jobSummaryMetric, jobConfig.Name),
	}
	resp, err := indexer.Index(metadataInfo, indexingOpts)
	if err != nil {
		log.Error(err)
	} else {
		log.Info(resp)
	}
}
