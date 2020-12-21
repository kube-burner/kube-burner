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

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
)

type metadata struct {
	Timestamp   time.Time  `json:"timestamp"`
	UUID        string     `json:"uuid"`
	MetricName  string     `json:"metricName"`
	ElapsedTime float64    `json:"elapsedTime"`
	JobConfig   config.Job `json:"jobConfig"`
}

const jobSummary = "jobSummary"

// IndexMetadataInfo Generates and indexes a document with metadata information of the passed job
func IndexMetadataInfo(indexer *indexers.Indexer, uuid string, elapsedTime float64, jobConfig config.Job, timestamp time.Time) {
	metadataInfo := []interface{}{
		metadata{
			UUID:        uuid,
			ElapsedTime: elapsedTime,
			JobConfig:   jobConfig,
			MetricName:  jobSummary,
			Timestamp:   timestamp,
		},
	}
	log.Infof("Indexing metadata information for job: %s", jobConfig.Name)
	(*indexer).Index(config.ConfigSpec.GlobalConfig.IndexerConfig.DefaultIndex, metadataInfo)
}
