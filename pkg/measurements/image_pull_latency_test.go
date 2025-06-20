package measurements

import (
	"testing"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestImagePullLatency(t *testing.T) {
	clientSet := fake.NewSimpleClientset()
	configSpec := config.Spec{
		GlobalConfig: config.GlobalConfig{
			UUID: "test-uuid",
		},
	}
	measurement := types.Measurement{
		Name: "imagePullLatency",
	}
	metadata := map[string]any{}

	factory, err := newImagePullLatencyMeasurementFactory(configSpec, measurement, metadata)
	if err != nil {
		t.Errorf("Failed to create measurement factory: %v", err)
	}

	jobConfig := &config.Job{
		Name: "test-job",
	}
	measurementInstance := factory.NewMeasurement(jobConfig, clientSet, nil)

	ipl := measurementInstance.(*imagePullLatency)

	// Test pod creation
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
			Labels: map[string]string{
				"kube-burner-runid": "test-uuid",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
				{
					Name:  "test-container-2",
					Image: "test-image-2",
				},
			},
		},
	}

	now := time.Now()

	// Test pod creation
	ipl.handleCreatePod(pod)

	// Test pod update with pulling state for both containers
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  "test-container",
			Image: "test-image",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "Pulling",
				},
			},
		},
		{
			Name:  "test-container-2",
			Image: "test-image-2",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason: "Pulling",
				},
			},
		},
	}
	ipl.handleUpdatePod(pod)

	// Set specific StartedAt values to simulate different pull times
	startedAt1 := metav1.NewTime(now.Add(200 * time.Millisecond))
	startedAt2 := metav1.NewTime(now.Add(400 * time.Millisecond))

	// Test pod update with running state for both containers
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{
			StartedAt: startedAt1,
		},
	}
	pod.Status.ContainerStatuses[1].State = corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{
			StartedAt: startedAt2,
		},
	}
	ipl.handleUpdatePod(pod)

	// Verify metrics for both containers
	metrics := ipl.metrics
	if value, exists := metrics.Load("test-uid"); exists {
		ipm := value.(imagePullMetric)
		containerMetric1, ok1 := ipm.ContainerMetrics["test-container"]
		containerMetric2, ok2 := ipm.ContainerMetrics["test-container-2"]
		if !ok1 {
			t.Errorf("Expected container metrics for 'test-container'")
		}
		if !ok2 {
			t.Errorf("Expected container metrics for 'test-container-2'")
		}
		if containerMetric1.PullLatency != 200 {
			t.Errorf("Expected pull latency 200ms for test-container, got %d", containerMetric1.PullLatency)
		}
		if containerMetric2.PullLatency != 400 {
			t.Errorf("Expected pull latency 400ms for test-container-2, got %d", containerMetric2.PullLatency)
		}
		if containerMetric1.ContainerName != "test-container" {
			t.Errorf("Expected container name 'test-container', got %s", containerMetric1.ContainerName)
		}
		if containerMetric2.ContainerName != "test-container-2" {
			t.Errorf("Expected container name 'test-container-2', got %s", containerMetric2.ContainerName)
		}
		if containerMetric1.Image != "test-image" {
			t.Errorf("Expected image 'test-image' for test-container, got %s", containerMetric1.Image)
		}
		if containerMetric2.Image != "test-image-2" {
			t.Errorf("Expected image 'test-image-2' for test-container-2, got %s", containerMetric2.Image)
		}
	} else {
		t.Error("Expected metrics to exist for test pod")
	}
}
