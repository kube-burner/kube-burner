package util

import (
	"bytes"
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func captureOutput(f func()) string {
	var buf bytes.Buffer
	logrus.SetOutput(&buf)
	defer logrus.SetOutput(os.Stderr)

	// Prevent log.Fatalf from exiting the test
	defer func() {
		_ = recover()
	}()

	logrus.SetLevel(logrus.InfoLevel)

	f()
	return buf.String()
}

func TestClusterHealthyVanillaK8s_AllNodesHealthy(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.NodeMemoryPressure,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.NodeDiskPressure,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   corev1.NodePIDPressure,
					Status: corev1.ConditionFalse,
				},
			},
		},
	})

	result := ClusterHealthyVanillaK8s(client)
	assert.True(t, result)
}

func TestClusterHealthyVanillaK8s_NodeNotReady(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-2",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	})

	result := ClusterHealthyVanillaK8s(client)
	assert.False(t, result)
}

func TestClusterHealthyVanillaK8s_MemoryPressure(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-3",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.NodeMemoryPressure,
					Status: corev1.ConditionTrue,
				},
			},
		},
	})

	result := ClusterHealthyVanillaK8s(client)
	assert.False(t, result)
}

func TestClusterHealthyVanillaK8s_NodeListError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "nodes", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, context.DeadlineExceeded
	})

	result := ClusterHealthyVanillaK8s(client)
	assert.False(t, result)
}

func TestClusterHealthCheck_HealthyCluster(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-healthy",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.NodeMemoryPressure,
					Status: corev1.ConditionFalse,
				},
			},
		},
	})

	logOutput := captureOutput(func() {
		ClusterHealthCheck(client)
	})

	assert.Contains(t, logOutput, "Checking for Cluster Health")
	assert.Contains(t, logOutput, "Cluster is healthy.")
}
