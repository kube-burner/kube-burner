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
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/burner"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
)

const (
	stepSize              = 30 * time.Second
	clusterMetadataMetric = "clusterMetadata"
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
	}
	return wh
}

func (wh *WorkloadHelper) Run(workload string, metricsProfiles []string, alertsProfiles []string) {
	var f io.Reader
	var rc int
	var err error
	var embedConfig bool
	var metricsScraper metrics.Scraper
	if wh.UserMetadata != "" {
		wh.Metadata.UserMetadata, err = util.ReadUserMetadata(wh.UserMetadata)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
		// Combine provided userMetadata with the regular OCP metadata
		for k, v := range wh.Metadata.UserMetadata {
			wh.MetricsMetadata[k] = v
		}
	}
	configFile := fmt.Sprintf("%s.yml", workload)
	if _, err := os.Stat(configFile); err != nil {
		f, err = util.ReadEmbedConfig(wh.embedConfig, path.Join(wh.ConfigDir, workload, configFile))
		embedConfig = true
		if err != nil {
			log.Fatalf("Error reading configuration file: %v", err.Error())
		}
	} else {
		log.Infof("Config file %v available in the current directory, using it", configFile)
		f, err = util.ReadConfig(configFile)
		if err != nil {
			log.Fatalf("Error reading configuration file %s: %s", configFile, err)
		}
	}
	ConfigSpec, err = config.Parse(wh.UUID, f)
	if err != nil {
		log.Fatal(err)
	}
	if embedConfig {
		ConfigSpec.EmbedFS = wh.embedConfig
		ConfigSpec.EmbedFSDir = path.Join(wh.ConfigDir, workload)
	}
	metricsScraper = metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
		ConfigSpec:      ConfigSpec,
		PrometheusStep:  stepSize,
		MetricsEndpoint: wh.MetricsEndpoint,
		MetricsProfiles: metricsProfiles,
		AlertProfiles:   alertsProfiles,
		SkipTLSVerify:   true,
		URL:             wh.PrometheusURL,
		Token:           wh.PrometheusToken,
		RawMetadata:     wh.MetricsMetadata,
		EmbedConfig:     embedConfig,
	})
	rc, err = burner.Run(ConfigSpec, wh.kubeClientProvider, metricsScraper, wh.Timeout)
	if err != nil {
		wh.Metadata.ExecutionErrors = err.Error()
		log.Error(err)
	}
	wh.Metadata.Passed = rc == 0
	for _, indexer := range metricsScraper.IndexerList {
		IndexMetadata(indexer, wh.Metadata)
	}
	log.Info("👋 Exiting kube-burner ", wh.UUID)
	os.Exit(rc)
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

// IndexMetadata indexes metadata using given indexer.
func IndexMetadata(indexer indexers.Indexer, metadata BenchmarkMetadata) {
	log.Info("Indexing cluster metadata document")
	metadata.EndDate = time.Now().UTC()
	msg, err := (indexer).Index([]interface{}{metadata}, indexers.IndexingOpts{
		MetricName: metadata.MetricName,
	})
	if err != nil {
		log.Error(err.Error())
	} else {
		log.Info(msg)
	}
}
