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
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/go-commons/version"
	"github.com/kube-burner/kube-burner/pkg/alerting"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
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
	namespace     string
	config.Object
}

// Executor contains the information required to execute a job
type Executor struct {
	objects []object
	config.Job
	uuid       string
	runid      string
	limiter    *rate.Limiter
	nsRequired bool
}

const (
	jobName              = "JobName"
	replica              = "Replica"
	jobIteration         = "Iteration"
	jobUUID              = "UUID"
	rcTimeout            = 2
	garbageCollectionJob = "garbage-collection"
)

var ClientSet *kubernetes.Clientset
var DynamicClient dynamic.Interface
var discoveryClient *discovery.DiscoveryClient
var restConfig *rest.Config
var embedFS embed.FS
var embedFSDir string

//nolint:gocyclo
func Run(configSpec config.Spec, prometheusClients []*prometheus.Prometheus, alertMs []*alerting.AlertManager, indexer *indexers.Indexer, timeout time.Duration, metadata map[string]interface{}) (int, error) {
	var err error
	var rc int
	var executedJobs []prometheus.Job
	var jobList []Executor
	var gcWg sync.WaitGroup
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
				job.QPS = rest.DefaultQPS
				job.Burst = rest.DefaultBurst
			} else {
				log.Infof("QPS: %v", job.QPS)
				log.Infof("Burst: %v", job.Burst)
			}
			ClientSet, restConfig, err = config.GetClientSet(job.QPS, job.Burst)
			if err != nil {
				log.Fatalf("Error creating clientSet: %s", err)
			}
			discoveryClient = discovery.NewDiscoveryClientForConfigOrDie(restConfig)
			DynamicClient = dynamic.NewForConfigOrDie(restConfig)
			if job.PreLoadImages && job.JobType == config.CreationJob {
				if err = preLoadImages(job); err != nil {
					log.Fatal(err.Error())
				}
			}
			currentJob := prometheus.Job{
				Start:     time.Now().UTC(),
				JobConfig: job.Job,
			}
			measurements.SetJobConfig(&job.Job)
			log.Infof("Triggering job: %s", job.Name)
			measurements.Start()
			switch job.JobType {
			case config.CreationJob:
				if job.Cleanup {
					// No timeout for initial job cleanup
					garbageCollectJob(context.TODO(), job, fmt.Sprintf("kube-burner-job=%s", job.Name), nil)
				}
				if job.Churn {
					log.Info("Churning enabled")
					log.Infof("Churn duration: %v", job.ChurnDuration)
					log.Infof("Churn percent: %v", job.ChurnPercent)
					log.Infof("Churn delay: %v", job.ChurnDelay)
					log.Infof("Churn deletion strategy: %v", job.ChurnDeletionStrategy)
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
			if job.BeforeCleanup != "" {
				log.Infof("Waiting for beforeCleanup command %s to finish", job.BeforeCleanup)
				cmd := exec.Command("/bin/sh", job.BeforeCleanup)
				var outb, errb bytes.Buffer
				cmd.Stdout = &outb
				cmd.Stderr = &errb
				err := cmd.Run()
				if err != nil {
					log.Infof("BeforeCleanup failed: %v", err)
				}
				log.Infof("BeforeCleanup out: %v, err: %v", outb.String(), errb.String())
			}
			if job.JobPause > 0 {
				log.Infof("Pausing for %v before finishing job", job.JobPause)
				time.Sleep(job.JobPause)
			}
			currentJob.End = time.Now().UTC()
			executedJobs = append(executedJobs, currentJob)
			// We stop and index measurements per job
			if !globalConfig.WaitWhenFinished {
				elapsedTime := currentJob.End.Sub(currentJob.Start).Round(time.Second)
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
		// We initialize garbage collection as soon as the benchmark finishes
		if globalConfig.GC {
			//nolint:govet
			gcCtx, _ := context.WithTimeout(context.Background(), globalConfig.GCTimeout)
			for _, job := range jobList {
				gcWg.Add(1)
				go garbageCollectJob(gcCtx, job, fmt.Sprintf("kube-burner-job=%s", job.Name), &gcWg)
			}
			if globalConfig.GCMetrics {
				cleanupStart := time.Now().UTC()
				log.Info("Garbage collection metrics on, waiting for GC")
				// If gcMetrics is enabled, garbage collection must be blocker
				gcWg.Wait()
				// We add an extra dummy job to executedJobs to index metrics from this stage
				executedJobs = append(executedJobs, prometheus.Job{
					Start: cleanupStart,
					End:   time.Now().UTC(),
					JobConfig: config.Job{
						Name: garbageCollectionJob,
					},
				})
			}
		}
		for _, job := range executedJobs {
			// elapsedTime is recalculated for every job of the list
			if indexer != nil && !job.JobConfig.SkipIndexing {
				jobTimings := timings{
					Timestamp:    job.Start,
					EndTimestamp: job.End,
					ElapsedTime:  job.End.Sub(job.Start).Round(time.Second).Seconds(),
				}
				indexjobSummaryInfo(indexer, uuid, jobTimings, job.JobConfig, metadata)
			}
		}
		for _, alertM := range alertMs {
			if err := alertM.Evaluate(executedJobs[0].Start, executedJobs[len(jobList)-1].End); err != nil {
				errs = append(errs, err)
				innerRC = 1
			}
		}
		for _, prometheusClient := range prometheusClients {
			prometheusClient.JobList = executedJobs
			// If prometheus is enabled query metrics from the start of the first job to the end of the last one
			if indexer != nil {
				prometheusClient.ScrapeJobsMetrics()
			}
		}
		if indexer != nil {
			if globalConfig.IndexerConfig.Type == indexers.LocalIndexer && globalConfig.IndexerConfig.CreateTarball {
				metrics.CreateTarball(globalConfig.IndexerConfig, globalConfig.IndexerConfig.TarballName)
			}
		}
		log.Infof("Finished execution with UUID: %s", uuid)
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
	// When GC is enabled and GCMetrics is disabled, we assume previous GC operation ran in background, so we have to ensure there's no garbage left
	if globalConfig.GC && !globalConfig.GCMetrics {
		log.Info("Garbage collecting jobs")
		gcWg.Wait()
	}
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
		sem := make(chan int, int(restConfig.QPS))
		for _, ns := range namespaces {
			sem <- 1
			wg.Add(1)
			go func(ns string) {
				executor.waitForObjects(ns, rate.NewLimiter(rate.Limit(restConfig.QPS), restConfig.Burst))
				<-sem
				wg.Done()
			}(ns)
		}
		wg.Wait()
	}
}

func garbageCollectJob(ctx context.Context, jobExecutor Executor, labelSelector string, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	CleanupNamespaces(ctx, labelSelector)
	for _, obj := range jobExecutor.objects {
		jobExecutor.limiter.Wait(ctx)
		if !obj.Namespaced {
			CleanupNonNamespacedResourcesUsingGVR(ctx, obj, labelSelector)
		} else if obj.namespace != "" { // When the object has a fixed namespace not generated by kube-burner
			CleanupNamespaceResourcesUsingGVR(ctx, obj, obj.namespace, labelSelector)
		}
	}
}
