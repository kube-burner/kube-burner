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
	"time"

	"github.com/vishnuchalla/go-commons/indexers"
	mtypes "github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
)

// JobType type of job
type JobType string

const (
	// CreationJob used to create objects
	CreationJob JobType = "create"
	// DeletionJob used to delete objects
	DeletionJob JobType = "delete"
	// PatchJob used to patch objects
	PatchJob JobType = "patch"
)

// Spec configuration root
type Spec struct {
	// GlobalConfig defines global configuration parameters
	GlobalConfig GlobalConfig `yaml:"global"`
	// Jobs list of kube-burner jobs
	Jobs []Job `yaml:"jobs"`
}

// GlobalConfig holds the global configuration
type GlobalConfig struct {
	// IndexerConfig contains a IndexerConfig definition
	IndexerConfig indexers.IndexerConfig `yaml:"indexerConfig"`
	// Measurements describes a list of measurements kube-burner
	// will take along with job
	Measurements []mtypes.Measurement `yaml:"measurements"`
	// RequestTimeout of restclient
	RequestTimeout time.Duration `yaml:"requestTimeout"`
	// PrometheusURL to interact with
	PrometheusURL string `yaml:"prometheusURL"`
	// BearerToken used to access prometheus
	BearerToken string `yaml:"bearerToken"`
	// MetricsProfile is the path to the metrics profile configuration
	MetricsProfile string `yaml:"metricsProfile"`
	// MetricsEndpoint is path to the metrics endpoint configuration YAML
	MetricsEndpoint string `yaml:"metricsEndpoint"`
	// AlertProfile is path to the alert profile
	AlertProfile string `yaml:"alertProfile"`
	// GC garbage collect created namespaces
	GC bool `yaml:"gc" json:"gc"`
}

// Object defines an object that kube-burner will create
type Object struct {
	// ObjectTemplate path to a valid YAML definition of a k8s resource
	ObjectTemplate string `yaml:"objectTemplate" json:"objectTemplate,omitempty"`
	// Replicas number of replicas to create of the given object
	Replicas int `yaml:"replicas" json:"replicas,omitempty"`
	// InputVars contains a map of arbitrary input variables
	// that can be introduced by users
	InputVars map[string]interface{} `yaml:"inputVars" json:"inputVars,omitempty"`
	// Kind object kind to delete
	Kind string `yaml:"kind" json:"kind,omitempty"`
	// The type of patch mode
	PatchType string `yaml:"patchType" json:"patchType,omitempty"`
	// APIVersion apiVersion of the object to remove
	APIVersion string `yaml:"apiVersion" json:"apiVersion,omitempty"`
	// LabelSelector objects with this labels will be removed
	LabelSelector map[string]string `yaml:"labelSelector" json:"labelSelector,omitempty"`
	// Namespaced this object is namespaced
	Namespaced bool `yaml:"namespaced" json:"namespaced"`
	// Wait for resource to be ready, it doesn't apply to all resources
	Wait bool `yaml:"wait" json:"wait"`
	// WaitOptions define custom behaviors when waiting for objects creation
	WaitOptions WaitOptions `yaml:"waitOptions" json:"waitOptions,omitempty"`
}

// Job defines a kube-burner job
type Job struct {
	// IterationCount how many times to execute the job
	JobIterations int `yaml:"jobIterations" json:"jobIterations,omitempty"`
	// IterationDelay how much time to wait between each job iteration
	JobIterationDelay time.Duration `yaml:"jobIterationDelay" json:"jobIterationDelay,omitempty"`
	// JobPause how much time to pause after finishing the job
	JobPause time.Duration `yaml:"jobPause" json:"jobPause,omitempty"`
	// Name job name
	Name string `yaml:"name" json:"name,omitempty"`
	// Objects list of objects
	Objects []Object `yaml:"objects" json:"objects,omitempty"`
	// JobType type of job
	JobType JobType `yaml:"jobType" json:"jobType,omitempty"`
	// Max number of queries per second
	QPS float32 `yaml:"qps" json:"qps,omitempty"`
	// Maximum burst for throttle
	Burst int `yaml:"burst" json:"burst,omitempty"`
	// Namespace namespace base name to use
	Namespace string `yaml:"namespace" json:"namespace,omitempty"`
	// MaxWaitTimeout maximum wait period
	MaxWaitTimeout time.Duration `yaml:"maxWaitTimeout" json:"maxWaitTimeout,omitempty"`
	// WaitForDeletion wait for objects to be definitively deleted
	WaitForDeletion bool `yaml:"waitForDeletion" json:"waitForDeletion,omitempty"`
	// PodWait wait for all pods to be running before moving forward to the next iteration
	PodWait bool `yaml:"podWait" json:"podWait,omitempty"`
	// WaitWhenFinished Wait for pods to be running when all job iterations are completed
	WaitWhenFinished bool `yaml:"waitWhenFinished" json:"waitWhenFinished,omitempty"`
	// Cleanup clean up old namespaces
	Cleanup bool `yaml:"cleanup" json:"cleanup,omitempty"`
	// NamespacedIterations create a namespace per job iteration
	NamespacedIterations bool `yaml:"namespacedIterations" json:"namespacedIterations,omitempty"`
	// IterationsPerNamespace is the modulus to apply to job iterations to calculate . Default 1
	IterationsPerNamespace int `yaml:"iterationsPerNamespace" json:"iterationsPerNamespace,omitempty"`
	// VerifyObjects verify object count after running the job
	VerifyObjects bool `yaml:"verifyObjects" json:"verifyObjects,omitempty"`
	// ErrorOnVerify exit when verification fails
	ErrorOnVerify bool `yaml:"errorOnVerify" json:"errorOnVerify,omitempty"`
	// PreLoadImages enables pulling all images before running the job
	PreLoadImages bool `yaml:"preLoadImages" json:"preLoadImages,omitempty"`
	// PreLoadPeriod determines the duration of the preload stage
	PreLoadPeriod time.Duration `yaml:"preLoadPeriod" json:"preLoadPeriod,omitempty"`
	// NamespaceLabels add custom labels to namespaces created by kube-burner
	NamespaceLabels map[string]string `yaml:"namespaceLabels" json:"namespaceLabels,omitempty"`
	// Churn workload
	Churn bool `yaml:"churn" json:"churn,omitempty"`
	// Churn percentage
	ChurnPercent int `yaml:"churnPercent" json:"churnPercent,omitempty"`
	// Churn duration
	ChurnDuration time.Duration `yaml:"churnDuration" json:"churnDuration,omitempty"`
	// Churn delay between sets
	ChurnDelay time.Duration `yaml:"churnDelay" json:"churnDelay,omitempty"`
}

type WaitOptions struct {
	// ForCondition wait for this condition to become true
	ForCondition string `yaml:"forCondition" json:"forCondition,omitempty"`
}
