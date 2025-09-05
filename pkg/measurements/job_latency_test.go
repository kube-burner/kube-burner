package measurements

import (
	"sync"
	"testing"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func toUnstructured(obj any) *unstructured.Unstructured {
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: unstructuredMap}
}

func getFakeJob(started, completed bool) batchv1.Job {
	conditions := []batchv1.JobCondition{}
	if completed {
		conditions = append(conditions, batchv1.JobCondition{
			Type:               batchv1.JobComplete,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(time.Now().Add(10 * time.Second)),
		})
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-job",
			Namespace:         "default",
			UID:               "test-uid",
			CreationTimestamp: metav1.NewTime(time.Now()),
			Labels: map[string]string{
				config.KubeBurnerLabelJobIteration: "1",
				config.KubeBurnerLabelReplica:      "1",
				"app":                              "test",
			},
		},
		Status: batchv1.JobStatus{
			StartTime:  &metav1.Time{Time: time.Now().Add(1 * time.Second)},
			Conditions: conditions,
		},
	}
	return job
}

func TestHandleCreateJob(t *testing.T) {
	j := &jobLatency{}
	j.metrics = sync.Map{}
	j.JobConfig = &config.Job{Name: "job-name"}
	j.Uuid = "uuid"

	job := getFakeJob(true, true)
	j.handleCreateJob(toUnstructured(&job))

	metricAny, ok := j.metrics.Load(string(job.UID))
	assert.True(t, ok)
	metric := metricAny.(jobMetric)
	assert.Equal(t, "uuid", metric.UUID)
	assert.Equal(t, "default", metric.Namespace)
	assert.Equal(t, "test-job", metric.Name)
	assert.Equal(t, 1, metric.JobIteration)
	assert.Equal(t, 1, metric.Replica)
}

func TestHandleUpdateJob(t *testing.T) {
	j := &jobLatency{}
	j.metrics = sync.Map{}
	job := getFakeJob(true, true)
	m := jobMetric{
		Timestamp: job.CreationTimestamp.Time,
		Namespace: job.Namespace,
		Name:      job.Name,
	}
	j.metrics.Store(string(job.UID), m)
	j.handleUpdateJob(toUnstructured(&job))

	updatedAny, ok := j.metrics.Load(string(job.UID))
	assert.True(t, ok)
	updated := updatedAny.(jobMetric)
	assert.False(t, updated.jobComplete.IsZero())
	assert.False(t, updated.startTime.IsZero())
}

func TestNormalizeMetrics(t *testing.T) {
	j := &jobLatency{}
	j.metrics = sync.Map{}
	j.normLatencies = make([]any, 0)

	start := time.Now()
	complete := start.Add(5 * time.Second)
	j.metrics.Store("key", jobMetric{
		Timestamp:   start,
		startTime:   start.Add(1 * time.Second),
		jobComplete: complete,
		Name:        "job-test",
	})

	latency := j.normalizeMetrics()
	assert.Equal(t, float64(0), latency)
	assert.Len(t, j.normLatencies, 1)

	m := j.normLatencies[0].(jobMetric)
	assert.Equal(t, 1000, m.StartTimeLatency)
	assert.Equal(t, 5000, m.CompletionLatency)
}

func TestGetLatency(t *testing.T) {
	j := &jobLatency{}
	metric := jobMetric{
		StartTimeLatency:  1000,
		CompletionLatency: 5000,
	}
	result := j.getLatency(metric)
	assert.Equal(t, 1000.0, result[jobStartTimeMeasurement])
	assert.Equal(t, 5000.0, result[string(batchv1.JobComplete)])
}

func TestCollect(t *testing.T) {
	job := getFakeJob(true, true)
	client := fake.NewSimpleClientset(&job)

	j := &jobLatency{
		BaseMeasurement: BaseMeasurement{
			ClientSet: client,
		},
	}
	j.JobConfig = &config.Job{
		Namespace:       "default",
		NamespaceLabels: map[string]string{"app": "test"},
		Name:            "job-name",
	}
	j.metrics = sync.Map{}

	var wg sync.WaitGroup
	wg.Add(1)
	go j.Collect(&wg)
	wg.Wait()

	m, ok := j.metrics.Load(string(job.UID))
	assert.True(t, ok)
	metric := m.(jobMetric)
	assert.Equal(t, "test-job", metric.Name)
	assert.False(t, metric.jobComplete.IsZero())
}
