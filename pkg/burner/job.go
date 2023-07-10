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

package burner

import (
	"context"
	"fmt"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	"github.com/cloud-bulldozer/kube-burner/pkg/version"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type object struct {
	gvr           schema.GroupVersionResource
	objectSpec    []byte
	labelSelector map[string]string
	patchType     string
	kind          string
	config.Object
}

// Executor contains the information required to execute a job
type Executor struct {
	objects []object
	Start   time.Time
	End     time.Time
	config.Job
	uuid    string
	limiter *rate.Limiter
}

const (
	jobName      = "JobName"
	replica      = "Replica"
	jobIteration = "Iteration"
	jobUUID      = "UUID"
	rcTimeout    = 2
)

var ClientSet *kubernetes.Clientset
var waitClientSet *kubernetes.Clientset
var dynamicClient dynamic.Interface
var waitDynamicClient dynamic.Interface
var discoveryClient *discovery.DiscoveryClient
var restConfig *rest.Config
var waitRestConfig *rest.Config

//nolint:gocyclo
func Run(configSpec config.Spec, prometheusClients []*prometheus.Prometheus, alertMs []*alerting.AlertManager, indexer *indexers.Indexer, timeout time.Duration, metadata map[string]interface{}) (int, error) {
	var err error
	var rc int
	var prometheusJobList []prometheus.Job
	res := make(chan int, 1)
	uuid := configSpec.GlobalConfig.UUID
	globalConfig := configSpec.GlobalConfig
	log.Infof("🔥 Starting kube-burner (%s@%s) with UUID %s", version.Version, version.GitCommit, uuid)
	go func() {
		var innerRC int
		measurements.NewMeasurementFactory(configSpec, indexer, metadata)
		jobList := newExecutorList(configSpec, uuid, timeout)
		// Iterate job list
		for jobPosition, job := range jobList {
			if job.QPS == 0 || job.Burst == 0 {
				log.Infof("QPS or Burst rates not set, using default client-go values: %v %v", rest.DefaultQPS, rest.DefaultBurst)
			} else {
				log.Infof("QPS: %v", job.QPS)
				log.Infof("Burst: %v", job.Burst)
			}
			ClientSet, restConfig, err = config.GetClientSet(job.QPS, job.Burst)
			discoveryClient = discovery.NewDiscoveryClientForConfigOrDie(restConfig)
			if err != nil {
				log.Fatalf("Error creating clientSet: %s", err)
			}
			dynamicClient = dynamic.NewForConfigOrDie(restConfig)
			if job.PreLoadImages {
				if err = preLoadImages(job); err != nil {
					log.Fatal(err.Error())
				}
			}
			jobList[jobPosition].Start = time.Now().UTC()
			measurements.SetJobConfig(&job.Job)
			log.Infof("Triggering job: %s", job.Name)
			measurements.Start()
			switch job.JobType {
			case config.CreationJob:
				if job.Cleanup {
					ctx, cancel := context.WithTimeout(context.Background(), timeout)
					defer cancel()
					CleanupNamespaces(ctx, v1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-job=%s", job.Name)}, true)
				}
				if job.Churn {
					log.Info("Churning enabled")
					log.Infof("Churn duration: %v", job.ChurnDuration)
					log.Infof("Churn percent: %v", job.ChurnPercent)
					log.Infof("Churn delay: %v", job.ChurnDelay)
				}
				job.RunCreateJob(0, job.JobIterations)
				// If object verification is enabled
				if job.VerifyObjects && !job.Verify() {
					errMsg := "Object verification failed"
					// If errorOnVerify is enabled. Set RC to 1
					if job.ErrorOnVerify {
						errMsg += ". Setting return code to 1"
						innerRC = 1
					}
					log.Error(errMsg)
				}
				if job.Churn {
					job.RunCreateJobWithChurn()
				}
			case config.DeletionJob:
				job.RunDeleteJob()
			case config.PatchJob:
				job.RunPatchJob()
			}
			if job.JobPause > 0 {
				log.Infof("Pausing for %v before finishing job", job.JobPause)
				time.Sleep(job.JobPause)
			}

			jobList[jobPosition].End = time.Now().UTC()
			prometheusJob := prometheus.Job{
				Start:     jobList[jobPosition].Start,
				End:       jobList[jobPosition].End,
				JobConfig: job.Job,
			}
			elapsedTime := prometheusJob.End.Sub(prometheusJob.Start).Round(time.Second)
			// Don't append to Prometheus jobList when prometheus it's not initialized
			if len(prometheusClients) > 0 {
				prometheusJobList = append(prometheusJobList, prometheusJob)
			}
			log.Infof("Job %s took %v", job.Name, elapsedTime)
			// We stop and index measurements per job
			if err = measurements.Stop(); err != nil {
				log.Errorf("Failed measurements: %v", err.Error())
				innerRC = 1
			}
		}
		if globalConfig.IndexerConfig.Type != "" {
			for _, job := range jobList {
				// elapsedTime is recalculated for every job of the list
				elapsedTime := job.End.Sub(job.Start).Round(time.Second).Seconds()
				indexjobSummaryInfo(indexer, uuid, elapsedTime, job.Job, job.Start, metadata)
			}
		}
		// We initialize cleanup as soon as the benchmark finishes
		if globalConfig.GC {
			go CleanupNamespaces(context.TODO(), v1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%v", uuid)}, false)
		}
		for idx, prometheusClient := range prometheusClients {
			// If alertManager is configured
			if alertMs[idx] != nil {
				if alertMs[idx].Evaluate(jobList[0].Start, jobList[len(jobList)-1].End) == 1 {
					innerRC = 1
				}
			}
			prometheusClient.JobList = prometheusJobList
			// If prometheus is enabled query metrics from the start of the first job to the end of the last one
			if globalConfig.IndexerConfig.Type != "" {
				metrics.ScrapeMetrics(prometheusClient, indexer)
				if globalConfig.IndexerConfig.Type == indexers.LocalIndexer && globalConfig.IndexerConfig.CreateTarball {
					metrics.CreateTarball(globalConfig.IndexerConfig)
				}
			}
		}
		log.Infof("Finished execution with UUID: %s", uuid)
		res <- innerRC
	}()
	select {
	case rc = <-res:
	case <-time.After(timeout):
		log.Errorf("%v timeout reached", timeout)
		rc = rcTimeout
	}
	if globalConfig.GC {
		// Use timeout/4 to garbage collect namespaces
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		log.Info("Garbage collecting remaining namespaces")
		CleanupNamespaces(ctx, v1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%v", uuid)}, true)
	}
	return rc, nil
}

// newExecutorList Returns a list of executors
func newExecutorList(configSpec config.Spec, uuid string, timeout time.Duration) []Executor {
	var ex Executor
	var executorList []Executor
	_, restConfig, err := config.GetClientSet(100, 100) // Hardcoded QPS/Burst
	if err != nil {
		log.Fatalf("Error creating clientSet: %s", err)
	}
	discoveryClient = discovery.NewDiscoveryClientForConfigOrDie(restConfig)
	for _, job := range configSpec.Jobs {
		if job.JobType == config.CreationJob {
			ex = setupCreateJob(job)
		} else if job.JobType == config.DeletionJob {
			ex = setupDeleteJob(&job)
		} else if job.JobType == config.PatchJob {
			ex = setupPatchJob(job)
		} else {
			log.Fatalf("Unknown jobType: %s", job.JobType)
		}
		for _, j := range executorList {
			if job.Name == j.Job.Name {
				log.Fatalf("Job names must be unique: %s", job.Name)
			}
		}
		job.MaxWaitTimeout = timeout
		// Limits the number of workers to QPS and Burst
		ex.limiter = rate.NewLimiter(rate.Limit(job.QPS), job.Burst)
		ex.Job = job
		ex.uuid = uuid
		executorList = append(executorList, ex)
	}
	return executorList
}
