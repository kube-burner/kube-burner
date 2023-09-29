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
	"embed"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/go-commons/version"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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
	runid   string
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
var DynamicClient dynamic.Interface
var waitDynamicClient dynamic.Interface
var discoveryClient *discovery.DiscoveryClient
var restConfig *rest.Config
var waitRestConfig *rest.Config
var embedFS embed.FS
var embedFSDir string

//nolint:gocyclo
func Run(configSpec config.Spec, prometheusClients []*prometheus.Prometheus, alertMs []*alerting.AlertManager, indexer *indexers.Indexer, timeout time.Duration, metadata map[string]interface{}) (int, error) {
	var err error
	var rc int
	var prometheusJobList []prometheus.Job
	var jobList []Executor
	var cleanupStart, cleanupEnd time.Time
	embedFS = configSpec.EmbedFS
	embedFSDir = configSpec.EmbedFSDir
	errs := []error{}
	res := make(chan int, 1)
	uuid := configSpec.GlobalConfig.UUID
	globalConfig := configSpec.GlobalConfig
	globalWaitMap := make(map[string][]string)
	executorMap := make(map[string]Executor)
	log.Infof("🔥 Starting kube-burner (%s@%s) with UUID %s", version.Version, version.GitCommit, uuid)
	go func() {
		var innerRC int
		measurements.NewMeasurementFactory(configSpec, indexer, metadata)
		jobList = newExecutorList(configSpec, uuid, timeout)
		// Iterate job list
		for jobPosition, job := range jobList {
			var waitListNamespaces []string
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
			DynamicClient = dynamic.NewForConfigOrDie(restConfig)
			if job.PreLoadImages && job.JobType == config.CreationJob {
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
					ctx, cancel := context.WithTimeout(context.Background(), globalConfig.GCTimeout)
					defer cancel()
					CleanupNamespaces(ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-job=%s", job.Name)}, true)
					CleanupNonNamespacedResourcesUsingGVR(ctx, jobList, true)
				}
				if job.Churn {
					log.Info("Churning enabled")
					log.Infof("Churn duration: %v", job.ChurnDuration)
					log.Infof("Churn percent: %v", job.ChurnPercent)
					log.Infof("Churn delay: %v", job.ChurnDelay)
				}
				job.RunCreateJob(0, job.JobIterations, &waitListNamespaces)
				// If object verification is enabled
				if job.VerifyObjects && !job.Verify() {
					err := errors.New("object verification failed")
					// If errorOnVerify is enabled. Set RC to 1 and append error
					if job.ErrorOnVerify {
						innerRC = 1
						errs = append(errs, err)
					}
					log.Error(err.Error())
				}
				if job.Churn {
					job.RunCreateJobWithChurn()
				}
				globalWaitMap[strconv.Itoa(jobPosition)+job.Name] = waitListNamespaces
				executorMap[strconv.Itoa(jobPosition)+job.Name] = job
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
			// Don't append to Prometheus jobList when prometheus it's not initialized
			if len(prometheusClients) > 0 {
				prometheusJob := prometheus.Job{
					Start:     jobList[jobPosition].Start,
					End:       jobList[jobPosition].End,
					JobConfig: job.Job,
				}
				prometheusJobList = append(prometheusJobList, prometheusJob)
			}
			// We stop and index measurements per job
			if !globalConfig.WaitWhenFinished {
				elapsedTime := jobList[jobPosition].End.Sub(jobList[jobPosition].Start).Round(time.Second)
				log.Infof("Job %s took %v", job.Name, elapsedTime)
				if err = measurements.Stop(); err != nil {
					errs = append(errs, err)
					log.Error(err.Error())
					innerRC = 1
				}
			}
		}
		if globalConfig.WaitWhenFinished {
			runWaitList(globalWaitMap, executorMap)
			if err = measurements.Stop(); err != nil {
				errs = append(errs, err)
				log.Error(err.Error())
				innerRC = 1
			}
		}
		cleanupStart = time.Now().UTC()
		// We initialize cleanup as soon as the benchmark finishes
		if globalConfig.GC {
			go CleanupNamespaces(context.TODO(), metav1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%v", uuid)}, false)
		}
		scrapeErrs := scrapeAndIndex(prometheusClients, alertMs, prometheusJobList, globalConfig.IndexerConfig, indexer, jobList[0].Start, jobList[len(jobList)-1].End)
		if len(scrapeErrs) > 0 {
			errs = append(errs, scrapeErrs...)
			innerRC = 1
		}
		res <- innerRC
	}()
	select {
	case rc = <-res:
	case <-time.After(timeout):
		err := fmt.Errorf("%v timeout reached", timeout)
		log.Errorf(err.Error())
		errs = append(errs, err)
		rc = rcTimeout
	}
	if globalConfig.GC {
		// Use timeout/4 to garbage collect namespaces
		ctx, cancel := context.WithTimeout(context.Background(), globalConfig.GCTimeout)
		defer cancel()
		log.Info("Garbage collecting remaining namespaces")
		CleanupNamespaces(ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%v", uuid)}, true)
		CleanupNonNamespacedResourcesUsingGVR(ctx, jobList, true)
	}
	cleanupEnd = time.Now().UTC()
	if globalConfig.IndexerConfig.Type != "" {
		for _, job := range jobList {
			indexJobSummaryInfo(indexer, uuid, job, cleanupStart, cleanupEnd, metadata)
		}
	}
	if globalConfig.GCMetrics {
		if configSpec.GlobalConfig.IndexerConfig.Type == indexers.LocalIndexer {
			prevMetricsDir := globalConfig.IndexerConfig.MetricsDirectory
			globalConfig.IndexerConfig.MetricsDirectory += "-cleanup"
			log.Infof("📁 Creating cleanup indexer: %s", indexers.LocalIndexer)
			indexer, err = indexers.NewIndexer(globalConfig.IndexerConfig)
			if err != nil {
				log.Fatalf("%v cleanup indexer: %v", indexers.LocalIndexer, err.Error())
			}
			globalConfig.IndexerConfig.MetricsDirectory = prevMetricsDir
		}
		log.Info("Attempting to collect metrics during garbage collection phase")
		for i := range prometheusJobList {
			prometheusJobList[i].Start = cleanupStart
			prometheusJobList[i].End = cleanupEnd
		}
		scrapeErrs := scrapeAndIndex(prometheusClients, alertMs, prometheusJobList, globalConfig.IndexerConfig, indexer, cleanupStart, cleanupEnd)
		if len(scrapeErrs) > 0 {
			errs = append(errs, scrapeErrs...)
		}
	}
	log.Infof("Finished execution with UUID: %s", uuid)
	return rc, utilerrors.NewAggregate(errs)
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
		switch job.JobType {
		case config.CreationJob:
			ex = setupCreateJob(job)
		case config.DeletionJob:
			ex = setupDeleteJob(job)
		case config.PatchJob:
			ex = setupPatchJob(job)
		default:
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
		ex.runid = configSpec.GlobalConfig.RUNID
		executorList = append(executorList, ex)
	}
	return executorList
}

// Runs on wait list at the end of benchmark
func runWaitList(globalWaitMap map[string][]string, executorMap map[string]Executor) {
	var wg sync.WaitGroup
	for executorUUID, namespaces := range globalWaitMap {
		executor := executorMap[executorUUID]
		log.Infof("Waiting up to %s for actions to be completed", executor.MaxWaitTimeout)
		// This semaphore is used to limit the maximum number of concurrent goroutines
		sem := make(chan int, int(ClientSet.RESTClient().GetRateLimiter().QPS())*2)
		for _, ns := range namespaces {
			sem <- 1
			wg.Add(1)
			go func(ns string) {
				executor.waitForObjects(ns)
				<-sem
				wg.Done()
			}(ns)
		}
		wg.Wait()
	}
}

func scrapeAndIndex(prometheusClients []*prometheus.Prometheus, alertMs []*alerting.AlertManager, prometheusJobList []prometheus.Job, indexerConfig indexers.IndexerConfig, indexer *indexers.Indexer, startTime, endTime time.Time) []error {
	errs := []error{}
	for idx, prometheusClient := range prometheusClients {
		// If alertManager is configured
		if alertMs[idx] != nil {
			if err := alertMs[idx].Evaluate(startTime, endTime); err != nil {
				errs = append(errs, err)
			}
		}
		prometheusClient.JobList = prometheusJobList
		// If prometheus is enabled query metrics from the start of the first job to the end of the last one
		if indexerConfig.Type != "" {
			metrics.ScrapeMetrics(prometheusClient, indexer)
			if indexerConfig.Type == indexers.LocalIndexer && indexerConfig.CreateTarball {
				metrics.CreateTarball(indexerConfig)
			}
		}
	}
	return errs
}
