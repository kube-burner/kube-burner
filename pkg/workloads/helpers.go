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
	"path"

	"github.com/kube-burner/kube-burner/pkg/burner"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
)

var ConfigSpec config.Spec

// NewWorkloadHelper initializes a WorkloadHelper with the provided configuration and embedded filesystem settings.
// It sets up the embedded configuration and returns a WorkloadHelper instance with initialized metadata maps.
//
// Parameters:
//   - config: The configuration settings for the workload.
//   - embedFS: The embedded filesystem containing workload and metrics directories.
//   - workloadDir: The directory path for workloads in the embedded filesystem.
//   - metricsDir: The directory path for metrics in the embedded filesystem.
//   - alertsDir: The directory path for alerts in the embedded filesystem.
//   - kubeClientProvider: The Kubernetes client provider for interacting with the cluster.
//
// Returns:
//   - A WorkloadHelper instance configured with the provided settings.
func NewWorkloadHelper(config Config, embedFS *embed.FS, workloadDir, metricsDir, alertsDir, scriptsDir string, kubeClientProvider *config.KubeClientProvider) WorkloadHelper {
	embedCfg := fileutils.NewEmbedConfiguration(embedFS, workloadDir, metricsDir, alertsDir, scriptsDir)
	wh := WorkloadHelper{
		Config:             config,
		kubeClientProvider: kubeClientProvider,
		MetricsMetadata:    make(map[string]any),
		SummaryMetadata:    make(map[string]any),
		EmbedCfg:           embedCfg,
	}
	return wh
}

// Run executes the workload based on the provided configuration file.
//
// Parameters:
//   - configFile: The path to the configuration file to be used for the workload.
//
// Returns:
//   - An integer representing the result of the workload execution.
func (wh *WorkloadHelper) Run(configFile string) int {
	return wh.RunWithAdditionalVars(configFile, nil, nil)
}

// RunWithAdditionalVars executes the workload based on the provided configuration file.
//
// Parameters:
//   - configFile: The path to the configuration file to be used for the workload.
//   - additionalVars: A map of additional variables to be used for variable substitution in the configuration file.
//   - additionalMeasurementFactoryMap: A map of additional measurement factories to be used for measurements.
//
// Returns:
//   - An integer representing the result of the workload execution.
func (wh *WorkloadHelper) RunWithAdditionalVars(configFile string, additionalVars map[string]any, additionalMeasurementFactoryMap map[string]measurements.NewMeasurementFactory) int {
	f, err := fileutils.GetWorkloadReader(configFile, wh.EmbedCfg)
	if err != nil {
		log.Fatalf("Error reading configuration file: %v", err.Error())
	}
	ConfigSpec, err = config.ParseWithUserdata(wh.UUID, wh.Timeout, f, nil, false, additionalVars)
	if err != nil {
		log.Fatal(err)
	}
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
	rc, err := burner.Run(ConfigSpec, wh.kubeClientProvider, metricsScraper, additionalMeasurementFactoryMap, wh.EmbedCfg)
	if err != nil {
		log.Error(err.Error())
	}
	log.Infof("👋 kube-burner run completed with rc %d for UUID %s", rc, wh.UUID)
	return rc
}

// ExtractWorkload extracts the given workload and metrics profile to the current directory
func ExtractWorkload(embedConfig embed.FS, embedFSDir string, workload string, rootCfg ...string) error {
	dirContent, err := embedConfig.ReadDir(path.Join(embedFSDir, workload))
	if err != nil {
		return err
	}
	workloadContent, _ := embedConfig.ReadFile(embedFSDir)
	if err = util.CreateFile(fmt.Sprintf("%v.yml", workload), workloadContent); err != nil {
		return err
	}
	for _, f := range dirContent {
		fileContent, _ := embedConfig.ReadFile(path.Join(embedFSDir, workload, f.Name()))
		err := util.CreateFile(f.Name(), fileContent)
		if err != nil {
			return err
		}
	}
	for _, f := range rootCfg {
		fileContent, _ := embedConfig.ReadFile(path.Join(embedFSDir, f))
		if err = util.CreateFile(f, fileContent); err != nil {
			return err
		}
	}
	return nil
}
