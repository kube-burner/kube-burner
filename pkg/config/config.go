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
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	uid "github.com/google/uuid"
	mtypes "github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var configSpec = Spec{
	GlobalConfig: GlobalConfig{
		RUNID:            uid.NewString(),
		GC:               false,
		GCMetrics:        false,
		GCTimeout:        1 * time.Hour,
		RequestTimeout:   60 * time.Second,
		Measurements:     []mtypes.Measurement{},
		WaitWhenFinished: false,
	},
}

// UnmarshalYAML unmarshals YAML data into the Indexer struct.
func (i *MetricsEndpoint) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawIndexer MetricsEndpoint
	indexer := rawIndexer{
		IndexerConfig: indexers.IndexerConfig{
			InsecureSkipVerify: false,
			MetricsDirectory:   "collected-metrics",
			TarballName:        "kube-burner-metrics.tgz",
		},
		SkipTLSVerify: true,
		Step:          30 * time.Second,
	}
	if err := unmarshal(&indexer); err != nil {
		return err
	}
	*i = MetricsEndpoint(indexer)
	return nil
}

// UnmarshalYAML implements Unmarshaller to customize object defaults
func (o *Object) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawObject Object
	object := rawObject{
		Wait: true,
	}
	if err := unmarshal(&object); err != nil {
		return err
	}
	*o = Object(object)
	return nil
}

// UnmarshalYAML implements Unmarshaller to customize job defaults
func (j *Job) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawJob Job
	raw := rawJob{
		Cleanup:                true,
		NamespacedIterations:   true,
		IterationsPerNamespace: 1,
		PodWait:                false,
		WaitWhenFinished:       true,
		VerifyObjects:          true,
		ErrorOnVerify:          true,
		JobType:                CreationJob,
		WaitForDeletion:        true,
		PreLoadImages:          true,
		PreLoadPeriod:          1 * time.Minute,
		Churn:                  false,
		ChurnCycles:            100,
		ChurnPercent:           10,
		ChurnDuration:          1 * time.Hour,
		ChurnDelay:             5 * time.Minute,
		ChurnDeletionStrategy:  "default",
	}

	if err := unmarshal(&raw); err != nil {
		return err
	}
	// Applying overrides here
	if configSpec.GlobalConfig.WaitWhenFinished {
		raw.PodWait = false
		raw.WaitWhenFinished = false
	}
	// Convert raw to Job
	*j = Job(raw)
	return nil
}

// Parse parses a configuration file
func Parse(uuid string, f io.Reader) (Spec, error) {
	cfg, err := io.ReadAll(f)
	if err != nil {
		return configSpec, fmt.Errorf("error reading configuration file: %s", err)
	}
	renderedCfg, err := util.RenderTemplate(cfg, util.EnvToMap(), util.MissingKeyError)
	if err != nil {
		return configSpec, fmt.Errorf("error rendering configuration template: %s", err)
	}
	cfgReader := bytes.NewReader(renderedCfg)
	yamlDec := yaml.NewDecoder(cfgReader)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&configSpec); err != nil {
		return configSpec, fmt.Errorf("error decoding configuration file: %s", err)
	}
	if err := jobIsDuped(); err != nil {
		return configSpec, err
	}
	if err := validateDNS1123(); err != nil {
		return configSpec, err
	}
	for i, job := range configSpec.Jobs {
		if len(job.Namespace) > 62 {
			log.Warnf("Namespace %s length has > 62 characters, truncating it", job.Namespace)
			configSpec.Jobs[i].Namespace = job.Namespace[:57]
		}
		if !job.NamespacedIterations && job.Churn {
			log.Fatal("Cannot have Churn enabled without Namespaced Iterations also enabled")
		}
		if job.JobIterations < 1 && (job.JobType == CreationJob || job.JobType == ReadJob) {
			log.Fatalf("Job %s has < 1 iterations", job.Name)
		}
		if job.JobType == DeletionJob {
			configSpec.Jobs[i].PreLoadImages = false
		}
	}
	configSpec.GlobalConfig.UUID = uuid
	return configSpec, nil
}

func NewKubeClientProvider(config, context string) *KubeClientProvider {
	var kubeConfigPath string
	if config != "" {
		kubeConfigPath = config
	} else if os.Getenv("KUBECONFIG") != "" {
		kubeConfigPath = os.Getenv("KUBECONFIG")
	} else if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); kubeConfigPath == "" && !os.IsNotExist(err) {
		kubeConfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	var restConfig *rest.Config
	var err error
	if kubeConfigPath == "" {
		if restConfig, err = rest.InClusterConfig(); err != nil {
			log.Fatalf("error preparing kubernetes client: %s", err)
		}
	} else {
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
			&clientcmd.ConfigOverrides{CurrentContext: context},
		)
		if restConfig, err = kubeConfig.ClientConfig(); err != nil {
			log.Fatalf("error preparing kubernetes client: %s", err)
		}
	}
	return &KubeClientProvider{restConfig: restConfig}
}

func (p *KubeClientProvider) DefaultClientSet() (kubernetes.Interface, *rest.Config) {
	restConfig := *p.restConfig
	return kubernetes.NewForConfigOrDie(&restConfig), &restConfig
}

func (p *KubeClientProvider) ClientSet(QPS float32, burst int) (kubernetes.Interface, *rest.Config) {
	restConfig := *p.restConfig
	restConfig.QPS, restConfig.Burst = QPS, burst
	restConfig.Timeout = configSpec.GlobalConfig.RequestTimeout
	return kubernetes.NewForConfigOrDie(&restConfig), &restConfig
}

func validateDNS1123() error {
	for _, job := range configSpec.Jobs {
		if errs := validation.IsDNS1123Subdomain(job.Name); len(errs) > 0 {
			return fmt.Errorf("Job %s name validation error: %s", job.Name, fmt.Sprint(errs))
		}
		if job.JobType == CreationJob && len(job.Namespace) > 0 {
			if errs := validation.IsDNS1123Subdomain(job.Namespace); job.JobType == CreationJob && len(errs) > 0 {
				return fmt.Errorf("Namespace %s name validation error: %s", job.Namespace, errs)
			}
		}
	}
	return nil
}

func jobIsDuped() error {
	jobCount := make(map[string]int)
	for _, job := range configSpec.Jobs {
		jobCount[job.Name]++
		if jobCount[job.Name] > 1 {
			return fmt.Errorf("Job names must be unique")
		}
	}
	return nil
}
