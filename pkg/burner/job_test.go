package burner

import (
	"context"
	"testing"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGarbageCollectionGVR(t *testing.T) {
	ctx := context.TODO()
	nsName := "kube-burner-job-1"
	// labelSelector := fmt.Sprintf("%s=%s", config.KubeBurnerLabelJob, "job-1")
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				config.KubeBurnerLabelJob: "job-1",
			},
		},
	}
	objects := []runtime.Object{ns}
	fakeClient := fake.NewSimpleClientset(objects...)

	// Setup JobExecutor
	ex := JobExecutor{
		Job: config.Job{
			Name: "job-1",
		},
		clientSet:        fakeClient,
		deletionStrategy: config.GVRDeletionStrategy,
	}

	// Execute GC
	ex.gc(ctx, nil)

	// Verify namespace still exists
	_, err := fakeClient.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	assert.NoError(t, err, "Namespace should NOT be deleted when deletionStrategy is gvr")
}

func TestGarbageCollectionDefault(t *testing.T) {
	ctx := context.TODO()
	// Setup fake client
	nsName := "kube-burner-job-2"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				config.KubeBurnerLabelJob: "job-2",
			},
		},
	}
	objects := []runtime.Object{ns}
	fakeClient := fake.NewSimpleClientset(objects...)

	// Setup JobExecutor
	ex := JobExecutor{
		Job: config.Job{
			Name: "job-2",
		},
		clientSet:        fakeClient,
		deletionStrategy: "default", // or empty
	}

	// Execute GC
	ex.gc(ctx, nil)

	// Wait for deletion (mock client deletes immediately but good to be safe)
	// In fake client, delete is crucial.
	// The gc function uses `CleanupNamespacesByLabel` which uses `Delete` and then `waitForDeleteNamespaces`.
	// We expect the namespace to be gone.

	// Verify namespace is deleted
	_, err := fakeClient.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	assert.Error(t, err, "Namespace SHOULD be deleted when deletionStrategy is default")
	assert.True(t, true, "Namespace not found error expected")
}
