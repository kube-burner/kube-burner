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
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	mtypes "github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"

	uid "github.com/google/uuid"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/client-go/tools/clientcmd"
)

var configSpec = Spec{
	GlobalConfig: GlobalConfig{
		RUNID:          uid.NewString(),
		GC:             false,
		GCMetrics:      false,
		GCTimeout:      1 * time.Hour,
		RequestTimeout: 60 * time.Second,
		Measurements:   []mtypes.Measurement{},
		IndexerConfig: indexers.IndexerConfig{
			InsecureSkipVerify: false,
			MetricsDirectory:   "collected-metrics",
			TarballName:        "kube-burner-metrics.tgz",
		},
		WaitWhenFinished: false,
	},
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
		MaxWaitTimeout:         4 * time.Hour,
		PreLoadImages:          true,
		PreLoadPeriod:          1 * time.Minute,
		Churn:                  false,
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
	if err != nil {
		return configSpec, err
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
		if job.JobIterations < 1 && job.JobType == CreationJob {
			log.Fatalf("Job %s has < 1 iterations", job.Name)
		}
		if job.JobType == DeletionJob {
			configSpec.Jobs[i].PreLoadImages = false
		}
	}
	configSpec.GlobalConfig.UUID = uuid
	if configSpec.GlobalConfig.IndexerConfig.MetricsDirectory == "collected-metrics" {
		configSpec.GlobalConfig.IndexerConfig.MetricsDirectory += "-" + uuid
	}
	return configSpec, nil
}

// FetchConfigMap Fetchs the specified configmap and looks for config.yml, metrics.yml and alerts.yml files
func FetchConfigMap(configMap, namespace string) (string, string, error) {
	log.Infof("Fetching configmap %s", configMap)
	var kubeconfig, metricProfile, alertProfile string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); kubeconfig == "" && !os.IsNotExist(err) {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return metricProfile, alertProfile, err
	}
	clientSet := kubernetes.NewForConfigOrDie(restConfig)
	configMapData, err := clientSet.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMap, v1.GetOptions{})
	if err != nil {
		return metricProfile, alertProfile, err
	}

	for name, data := range configMapData.Data {
		// We write the configMap data into the CWD
		if err := os.WriteFile(name, []byte(data), 0644); err != nil {
			return metricProfile, alertProfile, fmt.Errorf("Error writing configmap into disk: %v", err)
		}
		if name == "metrics.yml" {
			metricProfile = "metrics.yml"
		}
		if name == "alerts.yml" {
			alertProfile = "alerts.yml"
		}
	}
	return metricProfile, alertProfile, nil
}

func validateDNS1123() error {
	for _, job := range configSpec.Jobs {
		if errs := validation.IsDNS1123Subdomain(job.Name); len(errs) > 0 {
			return fmt.Errorf("Job %s name validation error: %s", job.Name, fmt.Sprint(errs))
		}
		if job.JobType == CreationJob {
			if errs := validation.IsDNS1123Subdomain(job.Namespace); job.JobType == CreationJob && len(errs) > 0 {
				return fmt.Errorf("Namespace %s name validation error: %s", job.Namespace, errs)
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
	} else if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); kubeconfig == "" && !os.IsNotExist(err) {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	restConfig, err = buildConfig(kubeconfig)
	if err != nil {
		return &kubernetes.Clientset{}, restConfig, err
	}
	restConfig.QPS, restConfig.Burst = QPS, burst
	restConfig.Timeout = configSpec.GlobalConfig.RequestTimeout
	return kubernetes.NewForConfigOrDie(restConfig), restConfig, nil
}

func buildConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		kubeconfig, err := rest.InClusterConfig()
		if err == nil {
			return kubeconfig, nil
		}
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: ""}}).ClientConfig()
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
