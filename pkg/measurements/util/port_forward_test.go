package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestCancelPodPortForwarder(t *testing.T) {
	ppf := &PodPortForwarder{
		PodName:  "test-pod",
		StopChan: make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		ppf.CancelPodPortForwarder()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("CancelPodPortForwarder did not close the stop channel in time")
	}
}

// TestNewPodPortForwarder_InvalidRoundTripper checks behavior when rest.Config is invalid.
func TestNewPodPortForwarder_InvalidRoundTripper(t *testing.T) {
	t.Skip("Skipping: requires SPDY support and valid RESTClient setup, not suitable for unit test")

	client := fake.NewSimpleClientset()
	invalidCfg := rest.Config{} // Incomplete config will fail

	ppf, err := NewPodPortForwarder(client, invalidCfg, "8080", "default", "fake-pod")
	assert.Error(t, err)
	assert.Empty(t, ppf.PodName)
}
