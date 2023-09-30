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

	log "github.com/sirupsen/logrus"
)

// ReadEmbedConfig reads configuration files from the given embed filesystem
func ReadEmbedConfig(embedFs embed.FS, configFile string) (io.Reader, error) {
	f, err := embedFs.Open(configFile)
	return f, err
}

// ReadConfig reads configuration from the given path or URL
func ReadConfig(configFile string) (io.Reader, error) {
	var f io.Reader
	f, err := os.Open(configFile)
	// If the template file does not exist we try to read it from an URL
	if os.IsNotExist(err) {
		log.Debugf("File %s not found, falling back to read from URL", configFile)
		f, err = readURL(configFile, f)
	}
	return f, err
}

// readURL reads an URL and returns a reader
func readURL(stringURL string, body io.Reader) (io.Reader, error) {
	u, err := url.Parse(stringURL)
	if err != nil {
		return body, err
	}
	if !u.IsAbs() {
		return body, fmt.Errorf("%s is not a valid URL", u)
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
