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

package util

import (
	"embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// ReadEmbedConfig reads configuration files from the given embed filesystem
func ReadEmbedConfig(embedFs embed.FS, configFile string) (io.Reader, error) {
	f, err := embedFs.Open(configFile)
	return f, err
}

// GetReaderForPath reads configuration from the given path or URL
func GetReaderForPath(path string) (io.Reader, error) {
	u, err := url.Parse(path)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		f, err := getBodyForURL(path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch config from URL %s: %w", path, err)
		}
		return f, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open local config file %s: %w", path, err)
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
