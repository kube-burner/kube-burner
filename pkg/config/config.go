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
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	mtypes "github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"

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
		MetricsDirectory: "collected-metrics",
		GC:               false,
		RequestTimeout:   15 * time.Second,
		WriteToFile:      false,
		Measurements:     []mtypes.Measurement{},
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

// UnmarshalYAML implements Unmarshaller to customize object defaults
func (o *Object) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawObject Object
	object := rawObject{
		Namespaced: true,
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
		Cleanup:              true,
		NamespacedIterations: true,
		PodWait:              false,
		WaitWhenFinished:     true,
		VerifyObjects:        true,
		ErrorOnVerify:        true,
		JobType:              CreationJob,
		WaitForDeletion:      true,
		MaxWaitTimeout:       3 * time.Hour,
		PreLoadImages:        true,
		PreLoadPeriod:        1 * time.Minute,
		Churn:                false,
		ChurnPercent:         10,
		ChurnDuration:        1 * time.Hour,
		ChurnDelay:           5 * time.Minute,
		Objects: []Object{
			{Namespaced: true},
		},
	}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	// Convert raw to Job
	*j = Job(raw)
	return nil
}

// Parse parses a configuration file
func Parse(c string, jobsRequired bool) (Spec, error) {
	f, err := util.ReadConfig(c)
	if err != nil {
		return configSpec, fmt.Errorf("Error reading configuration file %s: %s", c, err)
	}
	cfg, err := io.ReadAll(f)
	if err != nil {
		return configSpec, fmt.Errorf("Error reading configuration file %s: %s", c, err)
	}
	renderedCfg, err := renderConfig(cfg)
	if err != nil {
		return configSpec, err
	}
	cfgReader := bytes.NewReader(renderedCfg)
	yamlDec := yaml.NewDecoder(cfgReader)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&configSpec); err != nil {
		return configSpec, fmt.Errorf("Error decoding configuration file %s: %s", c, err)
	}
	if jobsRequired {
		if len(configSpec.Jobs) <= 0 {
			return configSpec, fmt.Errorf("No jobs found in the configuration file")
		}
		if err := validateDNS1123(); err != nil {
			return configSpec, err
		}
		for _, job := range configSpec.Jobs {
			if len(job.Namespace) > 62 {
				log.Warnf("Namespace %s length has > 63 characters, truncating it", job.Namespace)
				job.Namespace = job.Namespace[:57]
			}
			if !job.NamespacedIterations && job.Churn {
				log.Fatal("Cannot have Churn enabled without Namespaced Iterations also enabled")
			}
			if job.JobIterations < 1 && job.JobType == CreationJob {
				log.Fatal("Job %s has < 1 iterations")
			}
		}
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
