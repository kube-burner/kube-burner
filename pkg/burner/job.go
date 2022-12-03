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
	"fmt"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"github.com/cloud-bulldozer/kube-burner/pkg/version"
	"golang.org/x/time/rate"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type object struct {
	gvr            schema.GroupVersionResource
	objectTemplate string
	objectSpec     []byte
	replicas       int
	inputVars      map[string]interface{}
	labelSelector  map[string]string
	patchType      string
	namespaced     bool
	kind           string
}

// Executor contains the information required to execute a job
type Executor struct {
	objects   []object
	Start     time.Time
	End       time.Time
	Config    config.Job
	selector  *util.Selector
	uuid      string
	limiter   *rate.Limiter
	nsObjects bool
}

const (
	jobName      = "JobName"
	replica      = "Replica"
	jobIteration = "Iteration"
	jobUUID      = "UUID"
)

var ClientSet *kubernetes.Clientset
var dynamicClient dynamic.Interface
var restConfig *rest.Config

func Run(configSpec config.Spec, uuid string, p *prometheus.Prometheus, alertM *alerting.AlertManager) (int, error) {
	var rc int
	var err error
	var measurementsWg sync.WaitGroup
	var indexer *indexers.Indexer
	log.Infof("🔥 Starting kube-burner (%s@%s) with UUID %s", version.Version, version.GitCommit, uuid)
	if configSpec.GlobalConfig.IndexerConfig.Enabled {
		indexer, err = indexers.NewIndexer(configSpec)
		if err != nil {
			return 1, err
		}
	}
	measurements.NewMeasurementFactory(configSpec, uuid, indexer)
	jobList := newExecutorList(configSpec, uuid)
	// Iterate job list
	for jobPosition, job := range jobList {
		if job.Config.QPS == 0 || job.Config.Burst == 0 {
			log.Infof("QPS or Burst rates not set, using default client-go values: %v %v", rest.DefaultQPS, rest.DefaultBurst)
		} else {
			log.Infof("QPS: %v", job.Config.QPS)
			log.Infof("Burst: %v", job.Config.Burst)
		}
		ClientSet, restConfig, err = config.GetClientSet(job.Config.QPS, job.Config.Burst)
		if err != nil {
			log.Fatalf("Error creating clientSet: %s", err)
		}
		dynamicClient = dynamic.NewForConfigOrDie(restConfig)
		if job.Config.PreLoadImages {
			preLoadImages(job)
		}
		prometheusJob := prometheus.Job{
			Start: time.Now().UTC(),
		}
		jobList[jobPosition].Start = time.Now().UTC()
		log.Infof("Triggering job: %s", job.Config.Name)
		measurements.SetJobConfig(&job.Config)
		switch job.Config.JobType {
		case config.CreationJob:
			job.Cleanup()
			measurements.Start(&measurementsWg)
			measurementsWg.Wait()
			job.RunCreateJob(1, job.Config.JobIterations)
			// If object verification is enabled
			if job.Config.VerifyObjects && !job.Verify() {
				errMsg := "Object verification failed"
				// If errorOnVerify is enabled. Set RC to 1
				if job.Config.ErrorOnVerify {
					errMsg += ". Setting return code to 1"
					rc = 1
				}
				log.Error(errMsg)
			}
			if job.Config.Churn {
				job.RunCreateJobWithChurn()
			}
			// We stop and index measurements per job
			if measurements.Stop() == 1 {
				rc = 1
			}
		case config.DeletionJob:
			job.RunDeleteJob()
		case config.PatchJob:
			job.RunPatchJob()
		}
		if job.Config.JobPause > 0 {
			log.Infof("Pausing for %v before finishing job", job.Config.JobPause)
			time.Sleep(job.Config.JobPause)
		}
		prometheusJob.End = time.Now().UTC()
		elapsedTime := prometheusJob.End.Sub(prometheusJob.Start).Seconds()
		p.JobList = append(p.JobList, prometheusJob)
		log.Infof("Job %s took %.2f seconds", job.Config.Name, elapsedTime)
	}
	if configSpec.GlobalConfig.IndexerConfig.Enabled {
		for _, job := range jobList {
			elapsedTime := job.End.Sub(job.Start).Seconds()
			err := indexMetadataInfo(configSpec, indexer, uuid, elapsedTime, job.Config, job.Start)
			if err != nil {
				log.Errorf(err.Error())
			}
		}
	}
	if p != nil {
		log.Infof("Waiting %v extra before scraping prometheus", p.Step)
		time.Sleep(p.Step)
		// Update end time of last job
		jobList[len(jobList)-1].End = time.Now().UTC()
		// If alertManager is configured
		if alertM != nil {
			log.Infof("Evaluating alerts")
			if alertM.Evaluate(jobList[0].Start, jobList[len(jobList)-1].End) == 1 {
				rc = 1
			}
		}
		// If prometheus is enabled query metrics from the start of the first job to the end of the last one
		if len(p.MetricProfile) > 0 {
			if err := p.ScrapeJobsMetrics(indexer); err != nil {
				log.Error(err.Error())
			}
			if configSpec.GlobalConfig.WriteToFile && configSpec.GlobalConfig.CreateTarball {
				err = prometheus.CreateTarball(configSpec.GlobalConfig.MetricsDirectory)
				if err != nil {
					log.Error(err.Error())
				}
			}
		}
	}
	log.Infof("Finished execution with UUID: %s", uuid)
	if configSpec.GlobalConfig.GC {
		log.Info("Garbage collecting created namespaces")
		CleanupNamespaces(v1.ListOptions{LabelSelector: fmt.Sprintf("kube-burner-uuid=%v", uuid)})
	}
	log.Info("👋 Exiting kube-burner")
	return rc, nil
}

// newExecutorList Returns a list of executors
func newExecutorList(configSpec config.Spec, uuid string) []Executor {
	var ex Executor
	var executorList []Executor
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
			if job.Name == j.Config.Name {
				log.Fatalf("Job names must be unique: %s", job.Name)
			}
		}
		// Limits the number of workers to QPS and Burst
		ex.limiter = rate.NewLimiter(rate.Limit(job.QPS), job.Burst)
		ex.Config = job
		ex.uuid = uuid
		executorList = append(executorList, ex)
	}
	return executorList
}
