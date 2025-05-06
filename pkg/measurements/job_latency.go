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

package measurements

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	jobStartTimeMeasurement        = "StartTime"
	jobLatencyMeasurement          = "jobLatencyMeasurement"
	jobLatencyQuantilesMeasurement = "jobLatencyQuantilesMeasurement"
)

var (
	supportedJobConditions = map[string]struct{}{
		string(batchv1.JobComplete): {},
	}
)

type jobMetric struct {
	Timestamp         time.Time `json:"timestamp"`
	startTime         time.Time
	jobComplete       time.Time
	StartTimeLatency  int    `json:"startTimeLatency"`
	CompletionLatency int    `json:"completionLatency"`
	MetricName        string `json:"metricName"`
	UUID              string `json:"uuid"`
	JobName           string `json:"jobName,omitempty"`
	JobIteration      int    `json:"jobIteration"`
	Replica           int    `json:"replica"`
	Namespace         string `json:"namespace"`
	Name              string `json:"k8sJobName"`
	Metadata          any    `json:"metadata,omitempty"`
}

type jobLatency struct {
	BaseMeasurement
	watcher          *metrics.Watcher
	metrics          sync.Map
	latencyQuantiles []any
	normLatencies    []any
}

type jobLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newJobLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any) (MeasurementFactory, error) {
	if err := VerifyMeasurementConfig(measurement, supportedJobConditions); err != nil {
		return nil, err
	}
	return jobLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (jlmf jobLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) Measurement {
	return &jobLatency{
		BaseMeasurement: jlmf.NewBaseLatency(jobConfig, clientSet, restConfig),
	}
}

func (j *jobLatency) handleCreateJob(obj any) {
	job := obj.(*batchv1.Job)
	jobLabels := job.GetLabels()
	j.metrics.LoadOrStore(string(job.UID), jobMetric{
		Timestamp:    job.CreationTimestamp.Time.UTC(),
		Namespace:    job.Namespace,
		Name:         job.Name,
		MetricName:   jobLatencyMeasurement,
		UUID:         j.Uuid,
		JobName:      j.JobConfig.Name,
		Metadata:     j.Metadata,
		JobIteration: getIntFromLabels(jobLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(jobLabels, config.KubeBurnerLabelReplica),
	})
}

func (j *jobLatency) handleUpdateJob(obj any) {
	job := obj.(*batchv1.Job)
	if value, exists := j.metrics.Load(string(job.UID)); exists {
		jm := value.(jobMetric)
		if jm.jobComplete.IsZero() {
			for _, c := range job.Status.Conditions {
				if c.Status == corev1.ConditionTrue {
					switch c.Type {
					case batchv1.JobComplete:
						jm.startTime = job.Status.StartTime.Time
						jm.jobComplete = c.LastTransitionTime.Time.UTC()
					}
				}
			}
			j.metrics.Store(string(job.UID), jm)
		}
	}
}

// start jobLatency measurement
func (j *jobLatency) Start(measurementWg *sync.WaitGroup) error {
	// Reset latency slices, required in multi-job benchmarks
	j.latencyQuantiles, j.normLatencies = nil, nil
	defer measurementWg.Done()
	j.metrics = sync.Map{}
	log.Infof("Creating Job latency watcher for %s", j.JobConfig.Name)
	j.watcher = metrics.NewWatcher(
		j.ClientSet.BatchV1().RESTClient().(*rest.RESTClient),
		"jobWatcher",
		"jobs",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", j.Runid)
		},
		nil,
	)
	j.watcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: j.handleCreateJob,
		UpdateFunc: func(oldObj, newObj any) {
			j.handleUpdateJob(newObj)
		},
	})
	if err := j.watcher.StartAndCacheSync(); err != nil {
		log.Errorf("Job Latency measurement error: %s", err)
	}
	return nil
}

// collects job measurements triggered in the past
func (j *jobLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var jobs []batchv1.Job
	labelSelector := labels.SelectorFromSet(j.JobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	namespaces := strings.Split(j.JobConfig.Namespace, ",")
	for _, namespace := range namespaces {
		jobList, err := j.ClientSet.BatchV1().Jobs(namespace).List(context.TODO(), options)
		if err != nil {
			log.Errorf("error listing jobs in namespace %s: %v", namespace, err)
		}
		jobs = append(jobs, jobList.Items...)
	}
	j.metrics = sync.Map{}
	for _, job := range jobs {
		var startTime, completed time.Time
		for _, c := range job.Status.Conditions {
			switch c.Type {
			case batchv1.JobComplete:
				startTime = job.Status.StartTime.Time.UTC()
				completed = c.LastTransitionTime.Time.UTC()
			}
		}
		j.metrics.Store(string(job.UID), jobMetric{
			Timestamp:   job.Status.StartTime.Time.UTC(),
			Namespace:   job.Namespace,
			Name:        job.Name,
			MetricName:  jobLatencyMeasurement,
			UUID:        j.Uuid,
			jobComplete: completed,
			startTime:   startTime,
			JobName:     j.JobConfig.Name,
		})
	}
}

// Stop stops jobLatency measurement
func (j *jobLatency) Stop() error {
	var err error
	defer func() {
		if j.watcher != nil {
			j.watcher.StopWatcher()
		}
	}()
	j.normalizeMetrics()
	j.calcQuantiles()
	if len(j.Config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(j.Config.LatencyThresholds, j.latencyQuantiles)
	}
	for _, q := range j.latencyQuantiles {
		jq := q.(metrics.LatencyQuantiles)
		log.Infof("jobLatency @ %v: %v 99th: %v max: %v avg: %v", j.JobConfig.Name, jq.QuantileName, jq.P99, jq.Max, jq.Avg)
	}
	return err
}

// index sends metrics to the configured indexer
func (j *jobLatency) Index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]any{
		jobLatencyMeasurement:          j.normLatencies,
		jobLatencyQuantilesMeasurement: j.latencyQuantiles,
	}
	IndexLatencyMeasurement(j.Config, jobName, metricMap, indexerList)
}

func (j *jobLatency) GetMetrics() *sync.Map {
	return &j.metrics
}

func (j *jobLatency) normalizeMetrics() {
	j.metrics.Range(func(key, value any) bool {
		m := value.(jobMetric)
		// If a job does not reach the Complete state (this timestamp isn't set), we skip that job
		if m.jobComplete.IsZero() {
			log.Tracef("Job %v latency ignored as it did not reach Ready state", m.Name)
			return true
		}
		m.StartTimeLatency = int(m.startTime.Sub(m.Timestamp).Milliseconds())
		if m.StartTimeLatency < 0 {
			log.Tracef("StartTimeLatency for job %v falling under negative case. So explicitly setting it to 0", m.Name)
			m.StartTimeLatency = 0
		}
		m.CompletionLatency = int(m.jobComplete.Sub(m.Timestamp).Milliseconds())
		j.normLatencies = append(j.normLatencies, m)
		return true
	})
}

func (j *jobLatency) calcQuantiles() {
	getLatency := func(normLatency any) map[string]float64 {
		jobMetric := normLatency.(jobMetric)
		return map[string]float64{
			jobStartTimeMeasurement:     float64(jobMetric.StartTimeLatency),
			string(batchv1.JobComplete): float64(jobMetric.CompletionLatency),
		}
	}
	j.latencyQuantiles = CalculateQuantiles(j.Uuid, j.JobConfig.Name, j.Metadata, j.normLatencies, getLatency, jobLatencyQuantilesMeasurement)
}
