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
	"io"
	"os"
	"path"

	"github.com/kube-burner/kube-burner/pkg/burner"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
)

var ConfigSpec config.Spec

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(config Config, embedConfig embed.FS, kubeClientProvider *config.KubeClientProvider) WorkloadHelper {
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
	var f io.Reader
	var rc int
	var err error
	var embedConfig bool
	var metricsScraper metrics.Scraper
	configFile := fmt.Sprintf("%s.yml", workload)
	if _, err := os.Stat(configFile); err != nil {
		f, err = util.ReadEmbedConfig(wh.embedConfig, path.Join(wh.ConfigDir, workload, configFile))
		embedConfig = true
		if err != nil {
			log.Fatalf("Error reading configuration file: %v", err.Error())
		}
	} else {
		log.Infof("Config file %v available in the current directory, using it", configFile)
		f, err = util.GetReaderForPath(configFile)
		if err != nil {
			log.Fatalf("Error reading configuration file %s: %s", configFile, err)
		}
	}
	ConfigSpec, err = config.Parse(wh.UUID, wh.Timeout, f)
	if err != nil {
		log.Fatal(err)
	}
	if embedConfig {
		ConfigSpec.EmbedFS = wh.embedConfig
		ConfigSpec.EmbedFSDir = path.Join(wh.ConfigDir, workload)
	}
	// Overwrite credentials
	for pos := range ConfigSpec.MetricsEndpoints {
		ConfigSpec.MetricsEndpoints[pos].Endpoint = wh.PrometheusURL
		ConfigSpec.MetricsEndpoints[pos].Token = wh.PrometheusToken
	}
	metricsScraper = metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
		ConfigSpec:      &ConfigSpec,
		MetricsEndpoint: wh.MetricsEndpoint,
		SummaryMetadata: wh.SummaryMetadata,
		MetricsMetadata: wh.MetricsMetadata,
		EmbedConfig:     embedConfig,
		UserMetaData:    wh.UserMetadata,
	})
	rc, err = burner.Run(ConfigSpec, wh.kubeClientProvider, metricsScraper)
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
