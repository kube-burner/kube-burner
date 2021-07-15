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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ConfigSpec configuration object
var ConfigSpec Spec = Spec{
	GlobalConfig: GlobalConfig{
		MetricsDirectory: "collected-metrics",
		RequestTimeout:   15 * time.Second,
		WriteToFile:      true,
		Measurements:     []Measurement{},
		IndexerConfig: IndexerConfig{
			Enabled:            false,
			InsecureSkipVerify: false,
		},
	},
}

func envToMap() map[string]interface{} {
	envMap := make(map[string]interface{})
	for _, v := range os.Environ() {
		envVar := strings.SplitN(v, "=", 2)
		envMap[envVar[0]] = envVar[1]
	}
	return envMap
}

func renderConfig(cfg []byte) ([]byte, error) {
	rendered, err := util.RenderTemplate(cfg, envToMap(), util.MissingKeyError)
	if err != nil {
		return rendered, fmt.Errorf("Error rendering configuration template: %s", err)
	}
	return rendered, nil
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
		MaxWaitTimeout:       3 * time.Hour,
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	// Convert raw to Job
	*j = Job(raw)
	return nil
}

// Parse parses a configuration file
func Parse(c string, jobsRequired bool) error {
	f, err := util.ReadConfig(c)
	if err != nil {
		return fmt.Errorf("Error reading configuration file %s: %s", c, err)
	}
	cfg, err := ioutil.ReadAll(f)
	if err != nil {
		return fmt.Errorf("Error reading configuration file %s: %s", c, err)
	}
	renderedCfg, err := renderConfig(cfg)
	if err != nil {
		return err
	}
	cfgReader := bytes.NewReader(renderedCfg)
	yamlDec := yaml.NewDecoder(cfgReader)
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
		for _, m := range ConfigSpec.GlobalConfig.Measurements {
			switch m.Name {
			case string(podLatency):
				err = validatePodLatencyCfg(m)
			case string(pprof):
				err = validatePprofCfg(m)
			}
			if err != nil {
				log.Fatalf("Config validataion error: %s", err)
			}
		}
	}
	return nil
}

func validateDNS1123() error {
	for _, job := range ConfigSpec.Jobs {
		if errs := validation.IsDNS1123Subdomain(job.Name); len(errs) > 0 {
			return fmt.Errorf("Job %s name validation error: %s", job.Name, fmt.Sprint(errs))
		}
		if job.JobType == CreationJob {
			if errs := validation.IsDNS1123Subdomain(job.Namespace); job.JobType == CreationJob && len(errs) > 0 {
				return fmt.Errorf("Namespace %s name validation error: %s", job.Namespace, fmt.Sprint(errs))
			}
		}
	}
	return nil
}

// GetRestConfig returns restConfig with the given QPS and burst
func GetClientSet(QPS float32, burst int) (*kubernetes.Clientset, *rest.Config, error) {
	var err error
	var restConfig *rest.Config
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else if ConfigSpec.GlobalConfig.Kubeconfig != "" {
		kubeconfig = ConfigSpec.GlobalConfig.Kubeconfig
	} else if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); kubeconfig == "" && !os.IsNotExist(err) {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return &kubernetes.Clientset{}, restConfig, err
	}
	restConfig.QPS = QPS
	restConfig.Burst = burst
	restConfig.Timeout = ConfigSpec.GlobalConfig.RequestTimeout
	return kubernetes.NewForConfigOrDie(restConfig), restConfig, nil
}
