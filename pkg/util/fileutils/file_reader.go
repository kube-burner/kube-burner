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
	embedPath := filepath.Join(embedDir, location)
	f, err := embedFS.Open(embedPath)
	if err != nil {
		log.Infof("File %s not found in the embedded filesystem (tried: %s). Falling back to original path", location, embedPath)
		return getReader(location)
	}
	return f, nil
}

func getReader(location string) (io.Reader, error) {
	var f io.Reader
	u, err := url.Parse(location)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		f, err = getBodyForURL(location, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch config file from URL %s: %w", location, err)
		}
	} else {
		// Handle local file
		absPath, _ := filepath.Abs(location)

		// Check if path exists and validate it's a file, not a directory
		if stat, statErr := os.Stat(location); statErr == nil {
			if stat.IsDir() {
				return nil, fmt.Errorf("config file path is a directory, not a file: %s\n"+
					"  Absolute path: %s\n"+
					"  Please specify a file, not a directory", location, absPath)
			}
		}

		f, err = os.Open(location)
		if err != nil {
			return nil, formatFileError(location, absPath, err)
		}
	}
	return f, nil
}

func formatFileError(location, absPath string, err error) error {
	if os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s\n"+
			"  Absolute path: %s\n"+
			"  Please check:\n"+
			"  - The file path is correct\n"+
			"  - The file exists at the specified location\n"+
			"  - You have the correct working directory", location, absPath)
	}

	if os.IsPermission(err) {
		return fmt.Errorf("permission denied accessing config file: %s\n"+
			"  Absolute path: %s\n"+
			"  Please check:\n"+
			"  - You have read permissions for the file\n"+
			"  - The file is not locked by another process", location, absPath)
	}

	// Generic error with helpful context
	return fmt.Errorf("failed to open config file: %s\n"+
		"  Absolute path: %s\n"+
		"  Error: %v\n"+
		"  Please check the file path and permissions", location, absPath, err)
}

// getBodyForURL reads an URL and returns a reader
func getBodyForURL(stringURL string, body io.Reader) (io.Reader, error) {
	_, err := url.ParseRequestURI(stringURL)
	if err != nil {
		return body, fmt.Errorf("invalid URL format: %s\n"+
			"  Error: %v\n"+
			"  Please check the URL syntax", stringURL, err)
	}

	r, err := http.Get(stringURL)
	if err != nil {
		return body, fmt.Errorf("failed to fetch config from URL: %s\n"+
			"  Error: %v\n"+
			"  Please check:\n"+
			"  - The URL is accessible\n"+
			"  - Your network connection\n"+
			"  - The server is responding", stringURL, err)
	}

	if r.StatusCode != http.StatusOK {
		return body, fmt.Errorf("HTTP error fetching config from URL: %s\n"+
			"  Status: %d %s\n"+
			"  Please check:\n"+
			"  - The URL is correct\n"+
			"  - The file exists at the URL\n"+
			"  - You have access permissions", stringURL, r.StatusCode, http.StatusText(r.StatusCode))
	}

	return r.Body, nil
}
