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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/cloud-bulldozer/go-commons/v2/version"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	"github.com/kube-burner/kube-burner/pkg/watchers"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
)

// returnPair is a pair of return codes for a job
type returnPair struct {
	innerRC         int
	executionErrors string
}

const (
	jobName              = "JobName"
	replica              = "Replica"
	jobIteration         = "Iteration"
	jobUUID              = "UUID"
	jobRunId             = "RunID"
	rcTimeout            = 2
	rcAlert              = 3
	rcMeasurement        = 4
	garbageCollectionJob = "garbage-collection"
	APIVersionV1         = "v1"
)

var (
	supportedExecutionMode = map[config.ExecutionMode]struct{}{
		config.ExecutionModeParallel:   {},
		config.ExecutionModeSequential: {},
	}
)

// Runs the with the given configuration and metrics scraper, with the specified timeout.
// Returns:
// - error code
// - error
//
//nolint:gocyclo
func Run(configSpec config.Spec, kubeClientProvider *config.KubeClientProvider, metricsScraper metrics.Scraper, additionalMeasurementFactoryMap map[string]measurements.NewMeasurementFactory, embedCfg *fileutils.EmbedConfiguration) (int, error) {
	var err error
	var rc int
	var executedJobs []prometheus.Job
	var jobExecutors []JobExecutor
	var msWg, gcWg sync.WaitGroup
	var gcCtx context.Context
	var cancelGC context.CancelFunc
	errs := []error{}
	res := make(chan int, 1)
	uuid := configSpec.GlobalConfig.UUID
	globalConfig := configSpec.GlobalConfig
	globalWaitMap := make(map[string][]string)
	executorMap := make(map[string]JobExecutor)
	returnMap := make(map[string]returnPair)
	timeoutGCStarted := false
	log.Infof("🔥 Starting kube-burner (%s@%s) with UUID %s", version.Version, version.GitCommit, uuid)
	ctx, cancel := context.WithTimeout(context.Background(), configSpec.GlobalConfig.Timeout)
	defer cancel()
	go func() {
		var innerRC int
		clientSet, _ := kubeClientProvider.DefaultClientSet()
		measurementsFactory := measurements.NewMeasurementsFactory(configSpec, metricsScraper.MetricsMetadata, additionalMeasurementFactoryMap)
		jobExecutors = newExecutorList(configSpec, kubeClientProvider, embedCfg)
		handlePreloadImages(jobExecutors, kubeClientProvider)
		// Iterate job list
		var measurementsInstance *measurements.Measurements
		var measurementsJobName string
		for jobExecutorIdx, jobExecutor := range jobExecutors {
			executedJobs = append(executedJobs, prometheus.Job{
				Start:     time.Now().UTC(),
				JobConfig: jobExecutor.Job,
			})
			watcherManager := watchers.NewWatcherManager(clientSet, rate.NewLimiter(rate.Limit(jobExecutor.QPS), jobExecutor.Burst))
			for idx, watcher := range jobExecutor.Watchers {
				for replica := range watcher.Replicas {
					watcherManager.Start(watcher.Kind, watcher.LabelSelector, idx+1, replica+1)
				}
			}
			watcherStartErrors := watcherManager.Wait()
			slices.Concat(errs, watcherStartErrors)
			var waitListNamespaces []string
			if measurementsInstance == nil {
				measurementsJobName = jobExecutor.Name
				measurementsInstance = measurementsFactory.NewMeasurements(&jobExecutor.Job, kubeClientProvider, embedCfg)
				measurementsInstance.Start()
			}
			log.Infof("Triggering job: %s", jobExecutor.Name)
			if jobExecutor.JobType == config.CreationJob {
				if jobExecutor.Cleanup {
					// No timeout for initial job cleanup
					jobExecutor.gc(context.TODO(), nil)
				}
				if jobExecutor.Churn {
					log.Info("Churning enabled")
					log.Infof("Churn cycles: %v", jobExecutor.ChurnCycles)
					log.Infof("Churn duration: %v", jobExecutor.ChurnDuration)
					log.Infof("Churn percent: %v", jobExecutor.ChurnPercent)
					log.Infof("Churn delay: %v", jobExecutor.ChurnDelay)
					log.Infof("Churn deletion strategy: %v", jobExecutor.ChurnDeletionStrategy)
				}
				jobExecutor.RunCreateJob(ctx, 0, jobExecutor.JobIterations, &waitListNamespaces)
				if ctx.Err() != nil {
					return
				}
				// If object verification is enabled
				if jobExecutor.VerifyObjects && !jobExecutor.Verify() {
					err := errors.New("object verification failed")
					// If errorOnVerify is enabled. Set RC to 1 and append error
					if jobExecutor.ErrorOnVerify {
						innerRC = 1
						errs = append(errs, err)
					}
					log.Error(err.Error())
				}
				if jobExecutor.Churn {
					churnStart := time.Now().UTC()
					executedJobs[len(executedJobs)-1].ChurnStart = &churnStart
					jobExecutor.RunCreateJobWithChurn(ctx)
					churnEnd := time.Now().UTC()
					executedJobs[len(executedJobs)-1].ChurnEnd = &churnEnd
				}
				globalWaitMap[strconv.Itoa(jobExecutorIdx)+jobExecutor.Name] = waitListNamespaces
				executorMap[strconv.Itoa(jobExecutorIdx)+jobExecutor.Name] = jobExecutor
			} else {
				jobExecutor.Run(ctx)
				if ctx.Err() != nil {
					return
				}
			}
			if jobExecutor.BeforeCleanup != "" {
				log.Infof("Waiting for beforeCleanup command %s to finish", jobExecutor.BeforeCleanup)
				stdOut, stdErr, err := util.RunShellCmd(jobExecutor.BeforeCleanup, jobExecutor.embedCfg)
				if err != nil {
					err = fmt.Errorf("BeforeCleanup failed: %v", err)
					log.Error(err.Error())
					errs = append(errs, err)
					innerRC = 1
				}
				log.Infof("BeforeCleanup out: %v, err: %v", stdOut.String(), stdErr.String())
			}
			jobEnd := time.Now().UTC()
			if jobExecutor.MetricsClosing == config.AfterJob {
				executedJobs[len(executedJobs)-1].End = jobEnd
			}
			if jobExecutor.JobPause > 0 {
				log.Infof("Pausing for %v before finishing job", jobExecutor.JobPause)
				time.Sleep(jobExecutor.JobPause)
			}
			if jobExecutor.MetricsClosing == config.AfterJobPause {
				executedJobs[len(executedJobs)-1].End = time.Now().UTC()
			}
			if !globalConfig.WaitWhenFinished {
				elapsedTime := jobEnd.Sub(executedJobs[len(executedJobs)-1].Start).Round(time.Second)
				log.Infof("Job %s took %v", jobExecutor.Name, elapsedTime)
			}
			if !jobExecutor.MetricsAggregate {
				// We stop and index measurements per job
				if err = measurementsInstance.Stop(); err != nil {
					errs = append(errs, err)
					log.Error(err.Error())
					innerRC = rcMeasurement
				}
				if jobExecutor.MetricsClosing == config.AfterMeasurements {
					executedJobs[len(executedJobs)-1].End = time.Now().UTC()
				}
				if !jobExecutor.SkipIndexing && len(metricsScraper.IndexerList) > 0 {
					msWg.Add(1)
					go func(msi *measurements.Measurements, jobName string) {
						defer msWg.Done()
						msi.Index(jobName, metricsScraper.IndexerList)
					}(measurementsInstance, measurementsJobName)
				}
				measurementsInstance = nil
			}
			watcherStopErrs := watcherManager.StopAll()
			slices.Concat(errs, watcherStopErrs)
			if jobExecutor.GC {
				jobExecutor.gc(ctx, nil)
			}
		}
		if globalConfig.WaitWhenFinished {
			runWaitList(globalWaitMap, executorMap)
		}
		// We initialize garbage collection as soon as the benchmark finishes
		if globalConfig.GC {
			//nolint:govet
			gcCtx, _ = context.WithTimeout(context.Background(), globalConfig.GCTimeout)
			for _, jobExecutor := range jobExecutors {
				gcWg.Add(1)
				go jobExecutor.gc(gcCtx, &gcWg)
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
		// Make sure that measurements have indexed their stuff before we index metrics
		msWg.Wait()
		for _, job := range executedJobs {
			// Declare slice on each iteration
			var jobAlerts []error
			var executionErrors string
			for _, alertM := range metricsScraper.AlertMs {
				if err := alertM.Evaluate(job); err != nil {
					errs = append(errs, err)
					jobAlerts = append(jobAlerts, err)
					innerRC = rcAlert
				}
			}
			if len(jobAlerts) > 0 {
				executionErrors = utilerrors.NewAggregate(jobAlerts).Error()
			}
			returnMap[job.JobConfig.Name] = returnPair{innerRC: innerRC, executionErrors: executionErrors}
		}
		indexMetrics(uuid, executedJobs, returnMap, metricsScraper, configSpec, true, "", false)
		log.Infof("Finished execution with UUID: %s", uuid)
		res <- innerRC
	}()
	select {
	case rc = <-res:
	// When benchmark times out
	case <-time.After(configSpec.GlobalConfig.Timeout):
		err := fmt.Errorf("%v timeout reached", configSpec.GlobalConfig.Timeout)
		log.Error(err.Error())
		executedJobs[len(executedJobs)-1].End = time.Now().UTC()
		errs = append(errs, err)
		rc = rcTimeout
		if globalConfig.GC {
			gcCtx, cancelGC = context.WithTimeout(context.Background(), globalConfig.GCTimeout)
			defer cancelGC()
			for _, jobExecutor := range jobExecutors[:len(executedJobs)-1] {
				gcWg.Add(1)
				go jobExecutor.gc(gcCtx, &gcWg)
			}
			timeoutGCStarted = true
		}
		indexMetrics(uuid, executedJobs, returnMap, metricsScraper, configSpec, false, utilerrors.NewAggregate(errs).Error(), true)
	}
	if globalConfig.GC {
		// When GC is enabled and GCMetrics is disabled, we assume previous GC operation ran in background, so we have to ensure there's no garbage left
		// Also wait if timeout GC was started, regardless of GCMetrics setting
		if !globalConfig.GCMetrics || timeoutGCStarted {
			log.Info("Garbage collecting jobs")
			gcWg.Wait()
		}
		// When GC times out and job execution has finished successfully, return timeout
		if gcCtx.Err() == context.DeadlineExceeded && rc == 0 {
			errs = append(errs, fmt.Errorf("garbage collection timeout reached"))
			rc = rcTimeout
		}
	}
	return rc, utilerrors.NewAggregate(errs)
}

// If requests, preload the images used in the test into the node
func handlePreloadImages(executorList []JobExecutor, kubeClientProvider *config.KubeClientProvider) {
	clientSet, _ := kubeClientProvider.DefaultClientSet()
	for _, executor := range executorList {
		if executor.PreLoadImages && executor.JobType == config.CreationJob {
			if err := preLoadImages(executor, clientSet); err != nil {
				log.Fatal(err.Error())
			}
		}
	}
}

// indexMetrics indexes metrics for the executed jobs
func indexMetrics(uuid string, executedJobs []prometheus.Job, returnMap map[string]returnPair, metricsScraper metrics.Scraper, configSpec config.Spec, innerRC bool, executionErrors string, isTimeout bool) {
	var jobSummaries []JobSummary
	for _, job := range executedJobs {
		if !job.JobConfig.SkipIndexing {
			if value, exists := returnMap[job.JobConfig.Name]; exists && !isTimeout {
				innerRC = value.innerRC == 0
				executionErrors = value.executionErrors
			}
			jobSummaries = append(jobSummaries, JobSummary{
				UUID:                uuid,
				Timestamp:           job.Start,
				EndTimestamp:        job.End,
				ElapsedTime:         job.End.Sub(job.Start).Round(time.Second).Seconds(),
				ChurnStartTimestamp: job.ChurnStart,
				ChurnEndTimestamp:   job.ChurnEnd,
				JobConfig:           job.JobConfig,
				Metadata:            metricsScraper.SummaryMetadata,
				Passed:              innerRC,
				ExecutionErrors:     executionErrors,
				Version:             fmt.Sprintf("%v@%v", version.Version, version.GitCommit),
				MetricName:          jobSummaryMetric,
			})
		}
	}
	for _, indexer := range metricsScraper.IndexerList {
		IndexJobSummary(jobSummaries, indexer)
	}
	for _, prometheusClient := range metricsScraper.PrometheusClients {
		prometheusClient.ScrapeJobsMetrics(executedJobs...)
	}
	for _, indexer := range configSpec.MetricsEndpoints {
		if indexer.Type == indexers.LocalIndexer && indexer.CreateTarball {
			metrics.CreateTarball(indexer.IndexerConfig)
		}
	}
}

func verifyJobTimeout(job *config.Job, defaultTimeout time.Duration) {
	if job.MaxWaitTimeout == 0 {
		log.Debugf("job.MaxWaitTimeout is zero in %s, override by timeout: %s", job.Name, defaultTimeout)
		job.MaxWaitTimeout = defaultTimeout
	}
}

func verifyQPSBurst(job *config.Job) {
	if job.QPS <= 0 {
		log.Warnf("Invalid QPS (%v); using default: %v", job.QPS, rest.DefaultQPS)
		job.QPS = rest.DefaultQPS
	} else {
		log.Infof("QPS: %v", job.QPS)
	}

	if job.Burst <= 0 {
		log.Warnf("Invalid Burst (%v); using default: %v", job.Burst, rest.DefaultBurst)
		job.Burst = rest.DefaultBurst
	} else {
		log.Infof("Burst: %v", job.Burst)
	}
}

func verifyJobDefaults(job *config.Job, defaultTimeout time.Duration) {
	verifyJobTimeout(job, defaultTimeout)
	verifyQPSBurst(job)
}

// newExecutorList Returns a list of executors
func newExecutorList(configSpec config.Spec, kubeClientProvider *config.KubeClientProvider, embedCfg *fileutils.EmbedConfiguration) []JobExecutor {
	var executorList []JobExecutor
	for _, job := range configSpec.Jobs {
		verifyJobDefaults(&job, configSpec.GlobalConfig.Timeout)
		executorList = append(executorList, newExecutor(configSpec, kubeClientProvider, job, embedCfg))
	}
	return executorList
}

// Runs on wait list at the end of benchmark
func runWaitList(globalWaitMap map[string][]string, executorMap map[string]JobExecutor) {
	var wg sync.WaitGroup
	for executorUUID, namespaces := range globalWaitMap {
		executor := executorMap[executorUUID]
		log.Infof("Waiting up to %s for actions to be completed", executor.MaxWaitTimeout)
		// This semaphore is used to limit the maximum number of concurrent goroutines
		sem := make(chan int, int(executor.restConfig.QPS))
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

func (ex *JobExecutor) gc(ctx context.Context, wg *sync.WaitGroup) {
	labelSelector := fmt.Sprintf("kube-burner-job=%s", ex.Name)
	if wg != nil {
		defer wg.Done()
	}
	err := util.CleanupNamespaces(ctx, ex.clientSet, labelSelector)
	// Just report error and continue
	if err != nil {
		log.Error(err.Error())
	}
	for _, obj := range ex.objects {
		ex.limiter.Wait(ctx)
		if !obj.namespaced {
			CleanupNonNamespacedResourcesUsingGVR(ctx, *ex, obj, labelSelector)
		} else if obj.namespace != "" { // When the object has a fixed namespace not generated by kube-burner
			CleanupNamespaceResourcesUsingGVR(ctx, *ex, obj, obj.namespace, labelSelector)
		}
	}
}
