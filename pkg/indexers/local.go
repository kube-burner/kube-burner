package indexers

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
)

// Copyright 2023 The Kube-burner Authors.
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

const local = "local"

type Local struct {
	metricsDirectory string
}

func init() {
	indexerMap[local] = &Local{}
}

func (l *Local) new(configSpec config.Spec) error {
	if configSpec.GlobalConfig.IndexerConfig.MetricsDirectory == "" {
		return fmt.Errorf("directory name not specified")
	}
	l.metricsDirectory = configSpec.GlobalConfig.IndexerConfig.MetricsDirectory
	err := os.MkdirAll(l.metricsDirectory, 0744)
	return err
}

// Index uses generates a local file with the given name and metrics
func (l *Local) Index(documents []interface{}, opts IndexingOpts) {
	var metricName string
	if opts.JobName != "" {
		metricName = fmt.Sprintf("%s-%s.json", opts.MetricName, opts.JobName)
	} else {
		metricName = fmt.Sprintf("%s.json", opts.MetricName)
	}
	filename := path.Join(l.metricsDirectory, metricName)
	log.Infof("Writing metric to: %s", filename)
	f, err := os.Create(filename)
	if err != nil {
		log.Errorf("Error creating metrics file %s: %s", filename, err)
	}
	defer f.Close()
	jsonEnc := json.NewEncoder(f)
	if jsonEnc.Encode(documents); err != nil {
		log.Errorf("JSON encoding error: %s", err)
	}
}
