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
	"context"
	"fmt"

	"os"
	"path/filepath"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	mtypes "github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/client-go/tools/clientcmd"
)

var configSpec = Spec{
	GlobalConfig: GlobalConfig{
		GC:             false,
		RequestTimeout: 15 * time.Second,
		Measurements:   []mtypes.Measurement{},
		IndexerConfig: indexers.IndexerConfig{
			InsecureSkipVerify: false,
			MetricsDirectory:   "collected-metrics",
			TarballName:        "kube-burner-metrics.tgz",
		},
	},
}

// Parse parses a configuration file
func Parse(uuid, c string, jobsRequired bool) (Spec, error) {
	cfg := viper.New()
	cfg.SetConfigFile(c)
	if err := cfg.ReadInConfig(); err != nil {
		return configSpec, fmt.Errorf("failed to read config file: %s", err)
	}

	err := cfg.Unmarshal(&configSpec)
	if err != nil {
		return configSpec, fmt.Errorf("unable to unmarshal config file %v", err)
	}

	for i := range configSpec.Jobs {
		if !cfg.IsSet(fmt.Sprintf("jobs.%d.cleanup", i)) {
			configSpec.Jobs[i].Cleanup = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.namespacedIterations", i)) {
			configSpec.Jobs[i].NamespacedIterations = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.iterationsPerNamespace", i)) {
			configSpec.Jobs[i].IterationsPerNamespace = 1
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.podWait", i)) {
			configSpec.Jobs[i].PodWait = false
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.waitWhenFinished", i)) {
			configSpec.Jobs[i].WaitWhenFinished = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.verifyObjects", i)) {
			configSpec.Jobs[i].VerifyObjects = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.errorOnVerify", i)) {
			configSpec.Jobs[i].ErrorOnVerify = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.jobType", i)) {
			configSpec.Jobs[i].JobType = CreationJob
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.waitForDeletion", i)) {
			configSpec.Jobs[i].WaitForDeletion = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.maxWaitTimeout", i)) {
			configSpec.Jobs[i].MaxWaitTimeout = 3 * time.Hour
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.preLoadImages", i)) {
			configSpec.Jobs[i].PreLoadImages = true
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.churn", i)) {
			configSpec.Jobs[i].Churn = false
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.churnDuration", i)) {
			configSpec.Jobs[i].ChurnDuration = 1 * time.Hour
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.churnPercent", i)) {
			configSpec.Jobs[i].ChurnPercent = 10
		}

		if !cfg.IsSet(fmt.Sprintf("jobs.%d.churnDelay", i)) {
			configSpec.Jobs[i].ChurnDelay = 5 * time.Minute
		}

		for j := range configSpec.Jobs[i].Objects {
			if !cfg.IsSet(fmt.Sprintf("jobs.%d.objects.%d.wait", i, j)) {
				configSpec.Jobs[i].Objects[j].Wait = true
			}
		}

	}

	log.Printf("Check for defaults1234 %+v \n", cfg.GetString("jobs.0.jobType"))

	if jobsRequired {
		if len(configSpec.Jobs) <= 0 {
			//Error string should not be captilised
			return configSpec, fmt.Errorf("no jobs found in the configuration file")
		}
		if err := validateDNS1123(); err != nil {
			return configSpec, err
		}
		for _, job := range configSpec.Jobs {
			if len(job.Namespace) > 62 {
				log.Warnf("Namespace %s length has > 62 characters, truncating it", job.Namespace)
				job.Namespace = job.Namespace[:57]
			}
			if !job.NamespacedIterations && job.Churn {
				log.Fatal("Cannot have Churn enabled without Namespaced Iterations also enabled")
			}
			if job.JobIterations < 1 && job.JobType == CreationJob {
				log.Fatalf("Job %s has < 1 iterations", job.Name)
			}
		}
	}
	configSpec.GlobalConfig.UUID = uuid
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
