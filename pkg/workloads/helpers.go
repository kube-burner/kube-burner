// Copyright 2022 The Kube-burner Authors.
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

package workloads

import (
	"embed"
	"fmt"
	"os"
	"path"

	"github.com/kube-burner/kube-burner/pkg/burner"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
)

var ConfigSpec config.Spec

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(config Config, embedConfig *embed.FS, kubeClientProvider *config.KubeClientProvider) WorkloadHelper {
	if config.ConfigDir == "" {
		log.Fatal("Config dir cannot be empty")
	}
	wh := WorkloadHelper{
		Config:             config,
		embedConfig:        embedConfig,
		kubeClientProvider: kubeClientProvider,
		MetricsMetadata:    make(map[string]interface{}),
		SummaryMetadata:    make(map[string]interface{}),
	}
	return wh
}

func (wh *WorkloadHelper) Run(workload string) int {
	return wh.RunWithAdditionalVars(workload, nil, nil)
}

func (wh *WorkloadHelper) RunWithAdditionalVars(workload string, additionalVars map[string]interface{}, additionalMeasurementFactoryMap map[string]measurements.NewMeasurementFactory) int {
	configFile := fmt.Sprintf("%s.yml", workload)
	var embedFS *embed.FS
	var embedFSDir string
	if _, err := os.Stat(configFile); err != nil {
		embedFS = wh.embedConfig
		embedFSDir = path.Join(wh.ConfigDir, workload)
	}
	f, err := util.GetReader(configFile, embedFS, embedFSDir)
	if err != nil {
		log.Fatalf("Error reading configuration file: %v", err.Error())
	}
	ConfigSpec, err = config.ParseWithUserdata(wh.UUID, wh.Timeout, f, nil, false, additionalVars)
	if err != nil {
		log.Fatal(err)
	}
	// Set embedFS parameters according to where the configuration file was found
	ConfigSpec.EmbedFS = embedFS
	ConfigSpec.EmbedFSDir = embedFSDir
	// Overwrite credentials
	for pos := range ConfigSpec.MetricsEndpoints {
		ConfigSpec.MetricsEndpoints[pos].Endpoint = wh.PrometheusURL
		ConfigSpec.MetricsEndpoints[pos].Token = wh.PrometheusToken
	}
	metricsScraper := metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
		ConfigSpec:      &ConfigSpec,
		MetricsEndpoint: wh.MetricsEndpoint,
		SummaryMetadata: wh.SummaryMetadata,
		MetricsMetadata: wh.MetricsMetadata,
		UserMetaData:    wh.UserMetadata,
	})
	rc, err := burner.Run(ConfigSpec, wh.kubeClientProvider, metricsScraper, additionalMeasurementFactoryMap)
	if err != nil {
		log.Error(err.Error())
	}
	log.Infof("👋 kube-burner run completed with rc %d for UUID %s", rc, wh.UUID)
	return rc
}

// ExtractWorkload extracts the given workload and metrics profile to the current directory
func ExtractWorkload(embedConfig embed.FS, configDir string, workload string, rootCfg ...string) error {
	dirContent, err := embedConfig.ReadDir(path.Join(configDir, workload))
	if err != nil {
		return err
	}
	workloadContent, _ := embedConfig.ReadFile(configDir)
	if err = util.CreateFile(fmt.Sprintf("%v.yml", workload), workloadContent); err != nil {
		return err
	}
	for _, f := range dirContent {
		fileContent, _ := embedConfig.ReadFile(path.Join(configDir, workload, f.Name()))
		err := util.CreateFile(f.Name(), fileContent)
		if err != nil {
			return err
		}
	}
	for _, f := range rootCfg {
		fileContent, _ := embedConfig.ReadFile(path.Join(configDir, f))
		if err = util.CreateFile(f, fileContent); err != nil {
			return err
		}
	}
	return nil
}
