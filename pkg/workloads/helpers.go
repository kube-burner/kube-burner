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
	"io/fs"
	"os"
	"path"

	"github.com/kube-burner/kube-burner/v2/pkg/burner"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/v2/pkg/util/metrics"
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
		embedCfg:           embedCfg,
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
	f, err := fileutils.GetWorkloadReader(configFile, wh.embedCfg)
	if err != nil {
		log.Fatalf("Error reading configuration file: %v", err.Error())
	}
	ConfigSpec, err = config.ParseWithUserdata(wh.UUID, wh.Timeout, f, nil, false, additionalVars, nil)
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
		EmbedCfg:        wh.embedCfg,
	})
	rc, err := burner.Run(ConfigSpec, wh.kubeClientProvider, metricsScraper, additionalMeasurementFactoryMap, wh.embedCfg)
	if err != nil {
		log.Error(err.Error())
	}
	log.Infof("👋 kube-burner run completed with rc %d for UUID %s", rc, wh.UUID)
	return rc
}

// ExtractWorkload extracts the given workload and metrics profile from the embedded filesystem to the current directory.
// Parameters:
//   - embedConfig: The embedded filesystem containing workload and metrics directories.
//   - embedFSDir: The root directory of the embedded filesystem.
//   - folders: List of directories to be extracted.
func ExtractWorkload(embedConfig embed.FS, embedFSDir string, folders []string) error {
	for _, folder := range folders {
		subFS, err := fs.Sub(embedConfig, path.Join(embedFSDir, folder))
		if err != nil {
			return err
		}
		if err := extractDirectory(subFS, "."); err != nil {
			return err
		}
	}
	return nil
}

// Extracts a directory recursively
func extractDirectory(subFS fs.FS, dirPath string) error {
	dirContent, err := fs.ReadDir(subFS, dirPath)
	if err != nil {
		return err
	}
	for _, f := range dirContent {
		if f.IsDir() {
			err := os.MkdirAll(path.Join(dirPath, f.Name()), 0755)
			if err != nil {
				return err
			}
			extractDirectory(subFS, path.Join(dirPath, f.Name()))
			continue
		}
		filePath := path.Join(dirPath, f.Name())
		fileContent, err := fs.ReadFile(subFS, filePath)
		if err != nil {
			return err
		}
		if util.CreateFile(filePath, fileContent) != nil {
			return err
		}
	}
	return nil
}
