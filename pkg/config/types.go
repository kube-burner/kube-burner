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

	v1 "k8s.io/api/core/v1"
)

// JobType type of job
type JobType string

// MeasurementType type of measurement
type MeasurementType string

const (
	// CreationJob used to create objects
	CreationJob JobType = "create"
	// DeletionJob used to delete objects
	DeletionJob JobType         = "delete"
	podLatency  MeasurementType = "podLatency"
	pprof       MeasurementType = "pprof"
)

// Spec configuration root
type Spec struct {
	// GlobalConfig defines global configuration parameters
	GlobalConfig GlobalConfig `yaml:"global"`
	// Jobs list of kube-burner jobs
	Jobs []Job `yaml:"jobs"`
}

// IndexerConfig holds the indexer configuration
type IndexerConfig struct {
	// Type type of indexer
	Type string `yaml:"type"`
	// ESServers List of ElasticSearch instances
	ESServers []string `yaml:"esServers"`
	// DefaultIndex default index to send prometheus metrics
	DefaultIndex string `yaml:"defaultIndex"`
	// Port indexer port
	Port int `yaml:"port"`
	// InsecureSkipVerify disable TLS ceriticate verification
	InsecureSkipVerify bool `yaml:"insecureSkipVerify"`
	// Enabled enable indexer
	Enabled bool `yaml:"enabled"`
}

// LatencyThrehold holds the thresholds configuration
type LatencyThrehold struct {
	// ConditionType
	ConditionType v1.PodConditionType `yaml:"conditionType"`
	// Metric type
	Metric string `yaml:"metric"`
	// Threshold accepted
	Threshold time.Duration `yaml:"threshold"`
}

// PProftarget pprof targets to collect
type PProftarget struct {
	// Name pprof target name
	Name string `yaml:"name"`
	// Namespace pod namespace
	Namespace string `yaml:"namespace"`
	// LabelSelector get pprof from pods with these labels
	LabelSelector map[string]string `yaml:"labelSelector"`
	// BearerToken bearer token
	BearerToken string `yaml:"bearerToken"`
	// URL target URL
	URL string `yaml:"url"`
	// Cert Client certificate file
	Cert string `yaml:"cert"`
	// Key Private key file
	Key string `yaml:"key"`
}

// Measurement holds the measurement configuration
type Measurement struct {
	// Name is the name the measurement
	Name string `yaml:"name"`
	// ESIndex contains the ElasticSearch index used to
	// index the metrics
	ESIndex string `yaml:"esIndex"`
	// LatencyThreholds config
	LatencyThresholds []LatencyThrehold `yaml:"thresholds"`
	// PPRofTargets targets config
	PProfTargets []PProftarget `yaml:"pprofTargets"`
	// PPRofInterval pprof collect interval
	PProfInterval time.Duration `yaml:"pprofInterval"`
	// PProfDirectory output directory
	PProfDirectory string `yaml:"pprofDirectory"`
}

// GlobalConfig holds the global configuration
type GlobalConfig struct {
	// Kubeconfig path to a valid kubeconfig file
	Kubeconfig string `yaml:"kubeconfig"`
	// IndexerConfig contains a IndexerConfig definition
	IndexerConfig IndexerConfig `yaml:"indexerConfig"`
	// Write prometheus metrics to a file
	WriteToFile bool `yaml:"writeToFile"`
	// Create tarball
	CreateTarball bool `yaml:"createTarball"`
	// Directory to save metrics files in
	MetricsDirectory string `yaml:"metricsDirectory"`
	// Measurements describes a list of measurements kube-burner
	// will take along with job
	Measurements []Measurement `yaml:"measurements"`
	// RequestTimeout of restclient
	RequestTimeout time.Duration `yaml:"requestTimeout"`
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
	// APIVersion apiVersion of the object to remove
	APIVersion string `yaml:"apiVersion" json:"apiVersion,omitempty"`
	// LabelSelector objects with this labels will be removed
	LabelSelector map[string]string `yaml:"labelSelector" json:"labelSelector,omitempty"`
}

// Job defines a kube-burner job
type Job struct {
	// IterationCount how many times to execute the job
	JobIterations int `yaml:"jobIterations" json:"jobIterations"`
	// IterationDelay how much time to wait between each job iteration
	JobIterationDelay time.Duration `yaml:"jobIterationDelay" json:"jobIterationDelay"`
	// JobPause how much time to pause after finishing the job
	JobPause time.Duration `yaml:"jobPause" json:"jobPause"`
	// Name job name
	Name string `yaml:"name" json:"name"`
	// Objects list of objects
	Objects []Object `yaml:"objects" json:"objects"`
	// JobType type of job
	JobType JobType `yaml:"jobType" json:"jobType"`
	// Max number of queries per second
	QPS int `yaml:"qps" json:"qps"`
	// Maximum burst for throttle
	Burst int `yaml:"burst" json:"burst"`
	// Namespace namespace base name to use
	Namespace string `yaml:"namespace" json:"namespace"`
	// WaitFor list of objects to wait for, if not specified wait for all
	WaitFor []string `yaml:"waitFor" json:"waitFor"`
	// MaxWaitTimeout maximum wait period
	MaxWaitTimeout time.Duration `yaml:"maxWaitTimeout" json:"maxWaitTimeout"`
	// WaitForDeletion wait for objects to be definitively deleted
	WaitForDeletion bool `yaml:"waitForDeletion" json:"waitForDeletion"`
	// PodWait wait for all pods to be running before moving forward to the next iteration
	PodWait bool `yaml:"podWait" json:"podWait"`
	// WaitWhenFinished Wait for pods to be running when all job iterations are completed
	WaitWhenFinished bool `yaml:"waitWhenFinished" json:"waitWhenFinished"`
	// Cleanup clean up old namespaces
	Cleanup bool `yaml:"cleanup" json:"cleanup"`
	// NamespacedIterations create a namespace per job iteration
	NamespacedIterations bool `yaml:"namespacedIterations" json:"namespacedIterations"`
	// VerifyObjects verify object count after running the job
	VerifyObjects bool `yaml:"verifyObjects" json:"verifyObjects"`
	// ErrorOnVerify exit when verification fails
	ErrorOnVerify bool `yaml:"errorOnVerify" json:"errorOnVerify"`
}
