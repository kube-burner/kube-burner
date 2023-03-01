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
	"fmt"
	"os"
	"path"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
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
func indexjobSummaryInfo(configSpec config.Spec, indexer *indexers.Indexer, uuid string, elapsedTime float64, jobConfig config.Job, timestamp time.Time, metadata map[string]interface{}) error {
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
	if configSpec.GlobalConfig.WriteToFile {
		filename := fmt.Sprintf("%s-metadata.json", jobConfig.Name)
		if configSpec.GlobalConfig.MetricsDirectory != "" {
			if err := os.MkdirAll(configSpec.GlobalConfig.MetricsDirectory, 0744); err != nil {
				return fmt.Errorf("Error creating metrics directory: %v: ", err)
			}
			filename = path.Join(configSpec.GlobalConfig.MetricsDirectory, filename)
		}
		log.Infof("Writing to: %s", filename)
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("Error creating %s: %v", filename, err)
		}
		defer f.Close()
		jsonEnc := json.NewEncoder(f)
		if err := jsonEnc.Encode(metadataInfo); err != nil {
			return fmt.Errorf("JSON encoding error: %s", err)
		}
	}
	log.Infof("Indexing metadata information for job: %s", jobConfig.Name)
	(*indexer).Index(configSpec.GlobalConfig.IndexerConfig.DefaultIndex, metadataInfo)
	return nil
}
