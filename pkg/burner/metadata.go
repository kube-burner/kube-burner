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

	log "github.com/sirupsen/logrus"
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

// indexMetadataInfo Generates and indexes a document with metadata information of the passed job
func indexMetadataInfo(configSpec config.Spec, indexer *indexers.Indexer, uuid string, elapsedTime float64, jobConfig config.Job, timestamp time.Time) error {
	metadataInfo := []interface{}{
		metadata{
			UUID:        uuid,
			ElapsedTime: elapsedTime,
			JobConfig:   jobConfig,
			MetricName:  jobSummary,
			Timestamp:   timestamp,
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
