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
			},
		},
	}

	// Test pod creation
	ipl.handleCreatePod(pod)

	// Test pod update with pulling state
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
	}
	ipl.handleUpdatePod(pod)

	// Set a specific StartedAt value to simulate time passing
	startedAt := metav1.NewTime(time.Now().Add(-200 * time.Millisecond))

	// Test pod update with running state
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{
			StartedAt: startedAt,
		},
	}
	ipl.handleUpdatePod(pod)

	// Verify metrics
	metrics := ipl.metrics
	if value, exists := metrics.Load("test-uid"); exists {
		ipm := value.(imagePullMetric)
		containerMetric, ok := ipm.ContainerMetrics["test-container"]
		if !ok {
			t.Errorf("Expected container metrics for 'test-container'")
		}
		if containerMetric.PullLatency <= 0 {
			t.Errorf("Expected positive pull latency, got %d", containerMetric.PullLatency)
		}
		if containerMetric.ContainerName != "test-container" {
			t.Errorf("Expected container name 'test-container', got %s", containerMetric.ContainerName)
		}
		if containerMetric.Image != "test-image" {
			t.Errorf("Expected image 'test-image', got %s", containerMetric.Image)
		}
	} else {
		t.Error("Expected metrics to exist for test pod")
	}
}
