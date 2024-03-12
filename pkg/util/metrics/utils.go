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

package metrics

import (
	"bytes"
	"io"

	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Decodes metrics endpoint yaml file
func DecodeMetricsEndpoint(metricsEndpoint string, metricsEndpoints *[]metricEndpoint) {
	f, err := util.ReadConfig(metricsEndpoint)
	if err != nil {
		log.Fatalf("Error reading metricsEndpoint %s: %s", metricsEndpoint, err)
	}
	cfg, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("Error reading configuration file %s: %s", metricsEndpoint, err)
	}
	renderedME, err := util.RenderTemplate(cfg, util.EnvToMap(), util.MissingKeyError)
	if err != nil {
		log.Fatalf("Template error in %s: %s", metricsEndpoint, err)
	}
	yamlDec := yaml.NewDecoder(bytes.NewReader(renderedME))
	yamlDec.KnownFields(true)
	if err := yamlDec.Decode(&metricsEndpoints); err != nil {
		log.Fatalf("Error decoding metricsEndpoint %s: %s", metricsEndpoint, err)
	}
}
