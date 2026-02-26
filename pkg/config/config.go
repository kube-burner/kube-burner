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
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	uid "github.com/google/uuid"
	"github.com/kube-burner/kube-burner/v2/pkg/errors"
	mtypes "github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	DefaultDeletionStrategy = "default"
	GVRDeletionStrategy     = "gvr"
)

var configSpec = Spec{
	GlobalConfig: GlobalConfig{
		GC:                false,
		GCMetrics:         false,
		RequestTimeout:    60 * time.Second,
		Measurements:      []mtypes.Measurement{},
		WaitWhenFinished:  false,
		Timeout:           4 * time.Hour,
		FunctionTemplates: []string{},
		DeletionStrategy:  DefaultDeletionStrategy,
	},
}

// UnmarshalYAML unmarshals YAML data into the Indexer struct.
func (i *MetricsEndpoint) UnmarshalYAML(unmarshal func(any) error) error {
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

// UnmarshalYAML implements Unmarshaller to customize churn defaults
func (c *ChurnConfig) UnmarshalYAML(unmarshal func(any) error) error {
	type rawChurn ChurnConfig
	churn := rawChurn{
		Mode: ChurnNamespaces,
	}
	if err := unmarshal(&churn); err != nil {
		return err
	}
	*c = ChurnConfig(churn)
	return nil
}

// UnmarshalYAML implements Unmarshaller to customize object defaults
func (o *Object) UnmarshalYAML(unmarshal func(any) error) error {
	type rawObject Object
	object := rawObject{
		Wait:     true,
		Churn:    true,
		Replicas: 1,
	}
	if err := unmarshal(&object); err != nil {
		return err
	}
	*o = Object(object)
	return nil
}

// UnmarshalYAML implements Unmarshaller to customize watcher defaults
func (w *Watcher) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawWatcher Watcher
	watcher := rawWatcher{
		Replicas: 1,
	}
	if err := unmarshal(&watcher); err != nil {
		return err
	}
	*w = Watcher(watcher)
	return nil
}

// UnmarshalYAML implements Unmarshaller to customize job defaults
func (j *Job) UnmarshalYAML(unmarshal func(any) error) error {
	type rawJob Job
	raw := rawJob{
		Cleanup:                true,
		NamespacedIterations:   true,
		IterationsPerNamespace: 1,
		JobIterations:          1,
		PodWait:                false,
		WaitWhenFinished:       true,
		VerifyObjects:          true,
		ErrorOnVerify:          true,
		JobType:                CreationJob,
		WaitForDeletion:        true,
		PreLoadImages:          true,
		PreLoadPeriod:          10 * time.Minute,
		MetricsClosing:         AfterJobPause,
		Measurements:           []mtypes.Measurement{},
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

// deepMerge recursively merges src into dst, handling both maps and slices with numeric keys
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]

		if !exists {
			dst[key] = srcVal
			continue
		}

		// Check if dst value is a slice and src key might be numeric index access
		if dstSlice, dstIsSlice := dstVal.([]interface{}); dstIsSlice {
			if srcMap, srcIsMap := srcVal.(map[string]interface{}); srcIsMap {
				// src has numeric keys like {"0": {...}, "1": {...}}
				for idxStr, idxVal := range srcMap {
					if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 && idx < len(dstSlice) {
						// Merge into slice element
						if elemMap, elemIsMap := dstSlice[idx].(map[string]interface{}); elemIsMap {
							if idxValMap, idxValIsMap := idxVal.(map[string]interface{}); idxValIsMap {
								deepMerge(elemMap, idxValMap)
							} else {
								dstSlice[idx] = idxVal
							}
						} else {
							dstSlice[idx] = idxVal
						}
					}
				}
				continue
			}
		}

		// Both are maps - merge recursively
		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstMap, dstIsMap := dstVal.(map[string]interface{})
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
			continue
		}

		// Otherwise, override with src value
		dst[key] = srcVal
	}
}

// applySetVars merges setVars into the raw YAML map before unmarshaling to Spec
func applySetVars(configData []byte, setVars map[string]any) ([]byte, error) {
	// Parse YAML into a generic map
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(configData, &configMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config for merging: %v", err)
	}

	// Deep merge setVars into configMap
	deepMerge(configMap, setVars)

	// Marshal back to YAML
	mergedData, err := yaml.Marshal(configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged config: %v", err)
	}

	return mergedData, nil
}

func getInputData(userDataFileReader io.Reader, additionalVars map[string]any) (map[string]any, error) {
	// Get environment variables
	templateVars := util.EnvToMap()
	// If a userDataFileReader was provided use it to override values
	if userDataFileReader != nil {
		userDataFileVars := make(map[string]any)
		userData, err := io.ReadAll(userDataFileReader)
		if err != nil {
			return nil, fmt.Errorf("error reading user data file: %w", err)
		}

		if len(userData) == 0 {
			return nil, fmt.Errorf("user data file is empty. Please provide a valid YAML or JSON configuration file")
		}

		err = yaml.Unmarshal(userData, &userDataFileVars)
		if err != nil {
			return nil, errors.EnhanceYAMLParseError("user data file", err)
		}
		// Copy values from userDataFileVars to templateVars
		maps.Copy(templateVars, userDataFileVars)
	}
	// Copy additionalVars to templateVars
	maps.Copy(templateVars, additionalVars)
	return templateVars, nil
}

func Parse(uuid string, timeout time.Duration, configFileReader io.Reader) (Spec, error) {
	return ParseWithUserdata(uuid, timeout, configFileReader, nil, false, nil, nil)
}

// Parse parses a configuration file
func ParseWithUserdata(uuid string, timeout time.Duration, configFileReader, userDataFileReader io.Reader, allowMissingKeys bool, additionalVars map[string]any, setVars map[string]any) (Spec, error) {

	cfg, err := io.ReadAll(configFileReader)
	if err != nil {
		return configSpec, fmt.Errorf("error reading configuration file: %s", err)
	}

	if len(cfg) == 0 {
		return configSpec, fmt.Errorf("configuration file is empty. Please provide a valid YAML or JSON configuration file")
	}

	inputData, err := getInputData(userDataFileReader, additionalVars)
	inputData["UUID"] = uuid
	if err != nil {
		return configSpec, err
	}
	templateOptions := util.MissingKeyError
	if allowMissingKeys {
		templateOptions = util.MissingKeyZero
	}
	renderedCfg, err := util.RenderTemplate(cfg, inputData, templateOptions, []string{})
	if err != nil {
		return configSpec, fmt.Errorf("error rendering configuration template: %s", err)
	}

	// Apply --set overrides to the rendered config
	if len(setVars) > 0 {
		renderedCfg, err = applySetVars(renderedCfg, setVars)
		if err != nil {
			return configSpec, fmt.Errorf("error applying --set overrides: %s", err)
		}
		log.Tracef("Config after applying --set overrides:\n%s", string(renderedCfg))
	}

	cfgReader := bytes.NewReader(renderedCfg)
	yamlDec := yaml.NewDecoder(cfgReader)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&configSpec); err != nil {
		return configSpec, errors.EnhanceYAMLParseError("configuration file", err)
	}
	if err := jobIsDuped(); err != nil {
		return configSpec, err
	}
	if err := validateDNS1123(); err != nil {
		return configSpec, err
	}
	if err := validateGC(); err != nil {
		return configSpec, err
	}
	for i, job := range configSpec.Jobs {
		if len(job.Namespace) > 62 {
			log.Warnf("Namespace %s length has > 62 characters, truncating it", job.Namespace)
			configSpec.Jobs[i].Namespace = job.Namespace[:57]
		}
		if job.JobIterations < 1 && (job.JobType == CreationJob || job.JobType == ReadJob) {
			log.Fatalf("Job %s has < 1 iterations", job.Name)
		}
		if _, ok := metricsClosing[job.MetricsClosing]; !ok {
			log.Fatalf("Invalid value for metricsClosing: %s", job.MetricsClosing)
		}
		if job.JobType == DeletionJob {
			configSpec.Jobs[i].PreLoadImages = false
		}
	}
	configSpec.GlobalConfig.Timeout = timeout
	configSpec.GlobalConfig.UUID = uuid
	configSpec.GlobalConfig.RUNID = uid.NewString()
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
			log.Fatalf("no running cluster found (no kubeconfig and no in-cluster config): %v", err) // If no kubeconfig is provided or no env vars are set
		}
	} else {
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
			&clientcmd.ConfigOverrides{CurrentContext: context},
		)
		if restConfig, err = kubeConfig.ClientConfig(); err != nil {
			log.Fatalf("invalid kubeconfig or unreachable cluster: %v", err) // If kubeconfig is provided, but invalid or cluster is unreachable
		}

		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			log.Fatalf("unable to create Kubernetes client: %v", err) // If kubeconfig is provided, but unable to create client
		}
		if _, err := clientset.Discovery().ServerVersion(); err != nil {
			log.Fatalf("cluster config found but cannot reach API server: %v", err) // If kubeconfig is provided, but unable to reach API server
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
			return metricProfile, alertProfile, fmt.Errorf("error writing configmap into disk: %v", err)
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
		if job.JobType == CreationJob && len(job.Namespace) > 0 {
			if errs := validation.IsDNS1123Subdomain(job.Namespace); job.JobType == CreationJob && len(errs) > 0 {
				return fmt.Errorf("namespace %s name validation error: %s", job.Namespace, errs)
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

// validateGC checks if GC and global waitWhenFinished are enabled at the same time
func validateGC() error {
	if !configSpec.GlobalConfig.WaitWhenFinished {
		return nil
	}
	for _, job := range configSpec.Jobs {
		if job.GC {
			return fmt.Errorf("jobs GC and global waitWhenFinished cannot be enabled at the same time")
		}
	}
	return nil
}

// checks if Churn is enabled
func IsChurnEnabled(job Job) bool {
	return job.ChurnConfig.Duration > 0 || job.ChurnConfig.Cycles > 0
}

// setNestedValue sets a value in a nested map based on dot notation keys
func setNestedValue(m map[string]any, key string, value any) {
	keys := strings.Split(key, ".")
	current := m
	for i, k := range keys {
		if i == len(keys)-1 {
			current[k] = value
		} else {
			if _, ok := current[k]; !ok {
				current[k] = make(map[string]interface{})
			}
			current = current[k].(map[string]interface{})
		}
	}
}

// parseValue converts string value to appropriate type (int, float, bool, or string)
func parseValue(value string) any {
	// parse as integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		return int(intVal)
	}
	// parse as float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal
	}
	// parse as boolean
	if boolVal, err := strconv.ParseBool(value); err == nil {
		return boolVal
	}
	// Return as string
	return value
}

func ParseSetValues(setValues []string) (map[string]any, error) {
	result := make(map[string]interface{})
	for _, pair := range setValues {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --set value: %s. Must be in the format key=value", pair)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// Convert value to appropriate type
		setNestedValue(result, key, parseValue(value))
	}
	return result, nil
}
