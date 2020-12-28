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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/cloud-bulldozer/kube-burner/log"
)

// ReadConfig reads configuration from the given path or URL
func ReadConfig(configFile string) (io.Reader, error) {
	var f io.Reader
	f, err := os.Open(configFile)
	// If the template file does not exist we try to read it from an URL
	if os.IsNotExist(err) {
		log.Warnf("Configuration file %s not found, trying to read from URL", configFile)
		f, err = readURL(configFile)
	}
	return f, err
}

// readURL reads an URL and returns a reader
func readURL(stringURL string) (io.Reader, error) {
	var body io.Reader
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
		return body, fmt.Errorf("Error requesting %s: %d", u, r.StatusCode)
	}
	return r.Body, nil
}
