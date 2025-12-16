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

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	mtypes "github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"k8s.io/client-go/rest"
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
	// ReadJob used to read objects
	ReadJob JobType = "read"
	// KubeVirtJob used to send command to the KubeVirt service
	KubeVirtJob JobType = "kubevirt"
)

type KubeVirtOpType string

const (
	KubeVirtOpStart        KubeVirtOpType = "start"
	KubeVirtOpStop         KubeVirtOpType = "stop"
	KubeVirtOpRestart      KubeVirtOpType = "restart"
	KubeVirtOpPause        KubeVirtOpType = "pause"
	KubeVirtOpUnpause      KubeVirtOpType = "unpause"
	KubeVirtOpMigrate      KubeVirtOpType = "migrate"
	KubeVirtOpAddVolume    KubeVirtOpType = "add-volume"
	KubeVirtOpRemoveVolume KubeVirtOpType = "remove-volume"
)

// Spec configuration root
type Spec struct {
	// List of kube-burner indexers
	MetricsEndpoints []MetricsEndpoint `yaml:"metricsEndpoints"`
	// GlobalConfig defines global configuration parameters
	GlobalConfig GlobalConfig `yaml:"global"`
	// Jobs list of kube-burner jobs
	Jobs []Job `yaml:"jobs"`
}

// metricEndpoint describes prometheus endpoint to scrape
type MetricsEndpoint struct {
	indexers.IndexerConfig `yaml:"indexer"`
	Metrics                []string      `yaml:"metrics"`
	Alerts                 []string      `yaml:"alerts"`
	Endpoint               string        `yaml:"endpoint"`
	Step                   time.Duration `yaml:"step"`
	SkipTLSVerify          bool          `yaml:"skipTLSVerify"`
	Token                  string        `yaml:"token"`
	Username               string        `yaml:"username"`
	Password               string        `yaml:"password"`
	Alias                  string        `yaml:"alias"`
}

// GlobalConfig holds the global configuration
type GlobalConfig struct {
	// Benchmark UUID
	UUID string
	// Benchmark RUNID
	RUNID string
	// Measurements describes a list of measurements kube-burner
	// will take along with job
	Measurements []mtypes.Measurement `yaml:"measurements"`
	// RequestTimeout of restclient
	RequestTimeout time.Duration `yaml:"requestTimeout"`
	// GC garbage collect created namespaces
	GC bool `yaml:"gc" json:"gc"`
	// WaitWhenFinished Wait for pods to be running when all the jobs are completed
	WaitWhenFinished bool `yaml:"waitWhenFinished" json:"waitWhenFinished,omitempty"`
	// Boolean flag to collect metrics during garbage collection
	GCMetrics bool `yaml:"gcMetrics"`
	// Boolean flag to check for cluster-health
	ClusterHealth bool `yaml:"clusterHealth"`
	// Global Benchmark timeout
	Timeout time.Duration `yaml:"timeout"`
	// Function templates to render at runtime
	FunctionTemplates []string `yaml:"functionTemplates"`
	// DeletionStrategy global deletion strategy for all created objects
	DeletionStrategy string `yaml:"deletionStrategy" json:"deletionStrategy,omitempty"`
}

// Object defines an object that kube-burner will create
type Object struct {
	// ObjectTemplate path to a valid YAML definition of a k8s resource
	ObjectTemplate string `yaml:"objectTemplate" json:"objectTemplate,omitempty"`
	// Replicas number of replicas to create of the given object
	Replicas int `yaml:"replicas" json:"replicas,omitempty"`
	// InputVars contains a map of arbitrary input variables
	// that can be introduced by users
	InputVars map[string]any `yaml:"inputVars" json:"inputVars,omitempty"`
	// Kind object kind to delete
	Kind string `yaml:"kind" json:"kind,omitempty"`
	// The type of patch mode
	PatchType string `yaml:"patchType" json:"patchType,omitempty"`
	// APIVersion apiVersion of the object to remove
	APIVersion string `yaml:"apiVersion" json:"apiVersion,omitempty"`
	// LabelSelector objects with this labels will be removed
	LabelSelector map[string]string `yaml:"labelSelector" json:"labelSelector,omitempty"`
	// Wait for resource to be ready, it doesn't apply to all resources
	Wait bool `yaml:"wait" json:"wait"`
	// WaitOptions define custom behaviors when waiting for objects creation
	WaitOptions WaitOptions `yaml:"waitOptions" json:"waitOptions"`
	// Run Once to create the object only once incase of multiple iterative jobs
	RunOnce bool `yaml:"runOnce" json:"runOnce,omitempty"`
	// KubeVirt Operation
	KubeVirtOp KubeVirtOpType `yaml:"kubeVirtOp" json:"kubeVirtOp,omitempty"`
	// Churn object
	Churn bool `yaml:"churn" json:"churn,omitempty"`
}

// Job defines a kube-burner job
type Job struct {
	// IterationCount how many times to execute the job
	JobIterations int `yaml:"jobIterations" json:"jobIterations,omitempty"`
	// IterationDelay how much time to wait between each job iteration
	JobIterationDelay time.Duration `yaml:"jobIterationDelay" json:"jobIterationDelay,omitempty"`
	// JobPause how much time to pause after finishing the job
	JobPause time.Duration `yaml:"jobPause" json:"jobPause,omitempty"`
	// BeforeCleanup allows to run a bash script before the workload is deleted.
	BeforeCleanup string `yaml:"beforeCleanup" json:"beforeCleanup,omitempty"`
	// Name job name
	Name string `yaml:"name" json:"name,omitempty"`
	// Objects list of objects
	Objects []Object `yaml:"objects" json:"-"`
	// Watchers list of watchers
	Watchers []Watcher `yaml:"watchers" json:"-"`
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
	//  wait for all pods to be running before moving forward to the next iteration
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
	// PreLoadNodeLabels add node selector labels to resources in preload stage
	PreLoadNodeLabels map[string]string `yaml:"preLoadNodeLabels" json:"-"`
	// NamespaceLabels add custom labels to namespaces created by kube-burner
	NamespaceLabels map[string]string `yaml:"namespaceLabels" json:"-"`
	// NamespaceAnnotations add custom annotations to namespaces created by kube-burner
	NamespaceAnnotations map[string]string `yaml:"namespaceAnnotations" json:"-"`
	// ChurnConfig options
	ChurnConfig ChurnConfig `yaml:"churnConfig" json:"churnConfig,omitempty"`
	// Skip this job from indexing
	SkipIndexing               bool `yaml:"skipIndexing" json:"skipIndexing,omitempty"`
	DefaultMissingKeysWithZero bool `yaml:"defaultMissingKeysWithZero" json:"defaultMissingKeysWithZero,omitempty"`
	// Execute objects in a job either parallel or sequential. Default: parallel
	ExecutionMode ExecutionMode `yaml:"executionMode" json:"executionMode,omitempty"`
	// ObjectDelay how much time to wait between objects processing in patch jobs
	ObjectDelay time.Duration `yaml:"objectDelay" json:"objectDelay,omitempty"`
	// ObjectWait wait for each object to complete before processing the next one
	ObjectWait bool `yaml:"objectWait" json:"objectWait,omitempty"`
	// MetricsAggregate aggregate the metrics of this job with the next one
	MetricsAggregate bool `yaml:"metricsAggregate" json:"metricsAggregate,omitempty"`
	// MetricsClosing defines when to stop metrics collection
	MetricsClosing MetricsClosing `yaml:"metricsClosing" json:"metricsClosing,omitempty"`
	// Enables job's garbage collection
	GC bool `yaml:"gc" json:"gc"`
	// Measurements job-specific measurements to enable
	Measurements []mtypes.Measurement `yaml:"measurements" json:"measurements,omitempty"`
}

type WaitOptions struct {
	// APIVersion apiVersion to consider for wait
	APIVersion string `yaml:"apiVersion" json:"apiVersion,omitempty"`
	// Kind object kind to consider for wait
	Kind string `yaml:"kind" json:"kind,omitempty"`
	// LabelSelector objects with these labels will be considered
	LabelSelector map[string]string `yaml:"labelSelector" json:"labelSelector,omitempty"`
	// CustomStatusPaths defines the list of jq path specific status fields to check (e.g., [{"key":".[]conditions.type","value":"Available"}]).
	CustomStatusPaths []StatusPath `yaml:"customStatusPaths" json:"customStatusPaths,omitempty"`
}

type Watcher struct {
	// Kind object kind to consider for watch
	Kind string `yaml:"kind" json:"kind,omitempty"`
	// APIVersion object apiVersion to consider for watch
	APIVersion string `yaml:"apiVersion" json:"apiVersion,omitempty"`
	// LabelSelector objects with these labels will be considered
	LabelSelector map[string]string `yaml:"labelSelector" json:"labelSelector,omitempty"`
	// Replicas number of replicas to create of the given object
	Replicas int `yaml:"replicas" json:"replicas,omitempty"`
}

// StatusPath defines the structure for each key-value pair in CustomStatusPath.
type StatusPath struct {
	Key   string `yaml:"key" json:"key"`
	Value string `yaml:"value" json:"value"`
}

// Churn options
type ChurnConfig struct {
	// number of churn loop iterations
	Cycles int `yaml:"cycles" json:"cycles,omitempty"`
	// percentage of objects to churn
	Percent int `yaml:"percent" json:"percent,omitempty"`
	// duration of the churn stage
	Duration time.Duration `yaml:"duration" json:"duration,omitempty"`
	// Delay between sets
	Delay time.Duration `yaml:"delay" json:"delay,omitempty"`
	// Churning mode
	Mode ChurnMode `yaml:"mode" json:"mode,omitempty"`
}

type KubeClientProvider struct {
	restConfig *rest.Config
}

// Execution mode for Patch jobs
type ExecutionMode string

const (
	ExecutionModeParallel   ExecutionMode = "parallel"
	ExecutionModeSequential ExecutionMode = "sequential"
)

const (
	KubeBurnerLabelJob          = "kube-burner.io/job"
	KubeBurnerLabelUUID         = "kube-burner.io/uuid"
	KubeBurnerLabelRunID        = "kube-burner.io/runid"
	KubeBurnerLabelIndex        = "kube-burner.io/index"
	KubeBurnerLabelJobIteration = "kube-burner.io/job-iteration"
	KubeBurnerLabelReplica      = "kube-burner.io/replica"
	KubeBurnerLabelChurnDelete  = "kube-burner.io/churn-delete"
)

// MetricsCLosing strategy
type MetricsClosing string

const (
	AfterJobPause     MetricsClosing = "afterJobPause"
	AfterMeasurements MetricsClosing = "afterMeasurements"
	AfterJob          MetricsClosing = "afterJob"
)

var metricsClosing = map[MetricsClosing]struct{}{
	AfterJobPause:     {},
	AfterMeasurements: {},
	AfterJob:          {},
}

type ChurnMode string

const (
	ChurnNamespaces ChurnMode = "namespaces"
	ChurnObjects    ChurnMode = "objects"
)
