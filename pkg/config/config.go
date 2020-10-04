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
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ConfigSpec configuration object
var ConfigSpec Spec = Spec{
	GlobalConfig: GlobalConfig{
		MetricsDirectory: "collected-metrics",
		WriteToFile:      true,
		Measurements:     []Measurement{},
		IndexerConfig: IndexerConfig{
			Enabled:            false,
			InsecureSkipVerify: false,
		},
	},
}

// UnmarshalYAML implements Unmarshaller to customize defaults
func (j *Job) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawJob Job
	raw := rawJob{
		Cleanup:              true,
		NamespacedIterations: true,
		PodWait:              true,
		WaitWhenFinished:     false,
		VerifyObjects:        true,
		ErrorOnVerify:        false,
		JobType:              CreationJob,
		WaitForDeletion:      true,
		MaxWaitTimeout:       12 * time.Hour,
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	// Convert raw to Job
	*j = Job(raw)
	return nil
}

// Parse parses configuration file
func Parse(c string, jobsRequired bool) error {
	f, err := os.Open(c)
	if err != nil {
		return err
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&ConfigSpec); err != nil {
		return fmt.Errorf("Error decoding configuration file %s: %s", c, err)
	}
	if jobsRequired {
		if len(ConfigSpec.Jobs) <= 0 {
			return fmt.Errorf("No jobs found at configuration file")
		}
		if err := validateDNS1123(); err != nil {
			return err
		}
	}
	return nil
}

func validateDNS1123() error {
	for _, job := range ConfigSpec.Jobs {
		if errs := validation.IsDNS1123Subdomain(job.Name); len(errs) > 0 {
			return fmt.Errorf("Job %s name validation error: %s", job.Name, fmt.Sprint(errs))
		}
		if errs := validation.IsDNS1123Subdomain(job.Namespace); job.JobType == CreationJob && len(errs) > 0 {
			return fmt.Errorf("Namespace %s name validation error: %s", job.Namespace, fmt.Sprint(errs))
		}
	}
	return nil
}
