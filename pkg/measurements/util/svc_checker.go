package util

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

type SvcLatencyChecker struct {
	Pod        *corev1.Pod
	clientSet  kubernetes.Interface
	restConfig rest.Config
}

func NewSvcLatencyChecker(clientSet kubernetes.Interface, restConfig rest.Config) (SvcLatencyChecker, error) {
	pod, err := clientSet.CoreV1().Pods(types.SvcLatencyNs).Get(context.TODO(), types.SvcLatencyCheckerName, metav1.GetOptions{})
	if err != nil {
		return SvcLatencyChecker{}, err
	}
	return SvcLatencyChecker{
		Pod:        pod,
		clientSet:  clientSet,
		restConfig: restConfig,
	}, nil
}

func (lc *SvcLatencyChecker) Ping(address string, port int32, timeout time.Duration) error {
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Using BusyBox's sh (no bash in BusyBox)
	// Use the most basic and compatible approach for any BusyBox variant
	// Multiple methods for different busybox nc implementations:
	// 1. Try with -z (zero I/O mode) which most implementations support
	// 2. Without -z, use echo as input, works with all nc variants
	// Add timeout via loop+sleep which is compatible with all BusyBox versions
	cmd := []string{"sh", "-c", fmt.Sprintf(`
		for i in $(seq 1 200); do 
			# First try with -z flag (most compatible)
			nc -z %s %d 2>/dev/null && exit 0
			# If that fails, try echo | nc approach
			echo | nc %s %d 2>/dev/null && exit 0
			sleep 0.05
		done
		# If we get here, connection failed after all attempts
		exit 1`, address, port, address, port)}
	req := lc.clientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(lc.Pod.Name).
		Namespace(lc.Pod.Namespace).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: types.SvcLatencyCheckerName,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		Command:   cmd,
		TTY:       false,
	}, scheme.ParameterCodec)
	err := wait.PollUntilContextCancel(ctx, 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		exec, err := remotecommand.NewSPDYExecutor(&lc.restConfig, "POST", req.URL())
		if err != nil {
			return false, err
		}
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		})
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout waiting for endpoint %s:%d to be ready", address, port)
	}
	return err
}
