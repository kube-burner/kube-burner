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

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ConfigSpec struct {
	GlobalConfig GlobalConfig `yaml:"global,omitempty"`
	Jobs         []Job        `yaml:"jobs"`
}

type IndexerConfig struct {
	Enabled            bool     `yaml:"enabled"`
	Type               string   `yaml:"type"`
	ESServers          []string `yaml:"esServers"`
	DefaultIndex       string   `yaml:"defaultIndex"`
	Port               int      `yaml:"port"`
	Username           string   `yaml:"username"`
	Password           string   `yaml:"password"`
	InsecureSkipVerify bool     `yaml:"insecureSkipVerify"`
}

type Measurements struct {
	Name    string `yaml:"name"`
	ESIndex string `yaml:"esIndex"`
}

type GlobalConfig struct {
	// Kubeconfig path to a valid kubeconfig file
	Kubeconfig    string        `yaml:"kubeconfig"`
	IndexerConfig IndexerConfig `yaml:"indexerConfig"`
	// Write prometheus metrics to a file
	WriteToFile bool `yaml:"writeToFile"`
	// Directory to save metrics files in
	MetricsDirectory string `yaml:"metricsDirectory"`
	// Measurements metrics to grab
	Measurements []Measurements `yaml:"measurements"`
}

type Object struct {
	ObjectTemplate string            `yaml:"objectTemplate"`
	Replicas       int               `yaml:"replicas"`
	InputVars      map[string]string `yaml:"inputVars"`
}

type Job struct {
	// IterationCount how many times to execute the job
	JobIterations int `yaml:"jobIterations"`
	// IterationDelay how many milliseconds to wait between each job iteration
	JobIterationDelay int `yaml:"jobIterationDelay"`
	// IterationPause how many milliseconds to pause after finishing the job
	JobPause int `yaml:"jobPause"`
	// PodWait wait for all pods to be running before moving forward to the next iteration
	PodWait bool `yaml:"podWait"`
	// Cleanup clean up old namespaces
	Cleanup bool `yaml:"cleanup"`
	// Namespace namespace base name to use
	Namespace string `yaml:"namespace"`
	// whether to create namespaces or not with each iteration
	Namespaced bool `yaml:"namespaced"`
	// NamespacedIterations create a namespace per iteration
	NamespacedIterations bool `yaml:"namespacedIterations"`
	// Name name of the iteration
	Name string `yaml:"name"`
	// Objects list of objects
	Objects []Object `yaml:"objects"`
	// Max number of queries per second
	QPS int `yaml:"qps"`
	// Maximum burst for throttle
	Burst int `yaml:"burst"`
}

func Parse(c string, cfg *ConfigSpec) error {
	f, err := os.Open(c)
	if err != nil {
		return err
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(cfg); err != nil {
		return fmt.Errorf("Error decoding configuration file %s: %s", c, err)
	}
	return nil
}
