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

package fileutils

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type EmbedConfiguration struct {
	fs           *embed.FS
	workloadsDir string
	metricsDir   string
	alertsDir    string
	scriptsDir   string
}

func NewEmbedConfiguration(embedFS *embed.FS, embedWorkloadsDir, embedMetricsDir, embedAlertsDir, scriptsDir string) *EmbedConfiguration {
	return &EmbedConfiguration{
		fs:           embedFS,
		workloadsDir: embedWorkloadsDir,
		metricsDir:   embedMetricsDir,
		alertsDir:    embedAlertsDir,
		scriptsDir:   scriptsDir,
	}
}

func GetWorkloadReader(location string, embedCfg *EmbedConfiguration) (io.Reader, error) {
	if embedCfg != nil {
		return getEmbedReader(location, embedCfg.fs, embedCfg.workloadsDir)
	}
	return getReader(location)
}

func GetMetricsReader(location string, embedCfg *EmbedConfiguration) (io.Reader, error) {
	if embedCfg != nil {
		return getEmbedReader(location, embedCfg.fs, embedCfg.metricsDir)
	}
	return getReader(location)
}

func GetAlertsReader(location string, embedCfg *EmbedConfiguration) (io.Reader, error) {
	if embedCfg != nil {
		return getEmbedReader(location, embedCfg.fs, embedCfg.alertsDir)
	}
	return getReader(location)
}

func GetScriptsReader(location string, embedCfg *EmbedConfiguration) (io.Reader, error) {
	if embedCfg != nil {
		return getEmbedReader(location, embedCfg.fs, embedCfg.scriptsDir)
	}
	return getReader(location)
}

func getEmbedReader(location string, embedFS *embed.FS, embedDir string) (io.Reader, error) {
	log.Debugf("Looking for file %s in embed fs", location)
	f, err := embedFS.Open(filepath.Join(embedDir, location))
	if err != nil {
		log.Infof("File %s not found in the embedded filesystem. Falling back to original path", location)
		return getReader(location)
	}
	return f, nil
}

func getReader(location string) (io.Reader, error) {
	var f io.Reader
	u, err := url.Parse(location)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		f, err = getBodyForURL(location, nil)
	} else {
		f, err = os.Open(location)
		if err != nil {
			return nil, fmt.Errorf("failed to open config file %s: %w", location, err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open config file %s: %w", location, err)
	}
	return f, nil
}

// getBodyForURL reads an URL and returns a reader
func getBodyForURL(stringURL string, body io.Reader) (io.Reader, error) {
	u, err := url.ParseRequestURI(stringURL)
	if err != nil {
		return body, err
	}
	r, err := http.Get(stringURL)
	if err != nil {
		return body, err
	}
	if r.StatusCode != http.StatusOK {
		return body, fmt.Errorf("error requesting %s: %d", u, r.StatusCode)
	}
	return r.Body, nil
}
