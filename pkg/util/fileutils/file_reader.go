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

type embedConfiguration struct {
	FS           *embed.FS
	WorkloadsDir string
	MetricsDir   string
	AlertsDir    string
}

var EmbedCfg embedConfiguration

func SetEmbedConfiguration(embedFS *embed.FS, embedWorkloadsDir, embedMetricsDir, embedAlertsDir string) {
	EmbedCfg.FS = embedFS
	EmbedCfg.WorkloadsDir = embedWorkloadsDir
	EmbedCfg.MetricsDir = embedMetricsDir
	EmbedCfg.AlertsDir = embedAlertsDir
}

func GetWorkloadReader(location string) (io.Reader, error) {
	return getEmbedReader(location, EmbedCfg.WorkloadsDir)
}

func GetMetricsReader(location string) (io.Reader, error) {
	return getEmbedReader(location, EmbedCfg.MetricsDir)
}

func GetAlertsReader(location string) (io.Reader, error) {
	return getEmbedReader(location, EmbedCfg.AlertsDir)
}

func getEmbedReader(location, prefix string) (io.Reader, error) {
	if EmbedCfg.FS != nil {
		log.Debugf("Looking for file %s in embed fs", location)
		f, err := EmbedCfg.FS.Open(filepath.Join(prefix, location))
		if err != nil {
			log.Infof("File %s not found in the embedded filesystem. Falling back to original path", location)
			return GetReader(location)
		}
		return f, nil

	}
	return GetReader(location)
}

func GetReader(location string) (io.Reader, error) {
	var f io.Reader
	u, err := url.Parse(location)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		f, err = getBodyForURL(location, nil)
	} else {
		f, err = os.Open(location)
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
