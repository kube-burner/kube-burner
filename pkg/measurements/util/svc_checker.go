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
	// Use the wrapper script we created during setup-service-checker
	// This provides a consistent and reliable way to connect across different BusyBox variants
	cmd := []string{"sh", "-c", fmt.Sprintf(`
		# Maximum retry attempts
		max_attempts=200
		# Sleep interval between attempts (50ms)
		sleep_interval=0.05
		
		# Source the PATH if it exists
		if [ -f /tmp/nc_path.sh ]; then
			. /tmp/nc_path.sh
		fi
		
		# Check if our wrapper script exists
		if [ -x /tmp/nc-wrapper.sh ]; then
			# Use the wrapper script that handles all netcat variants
			for i in $(seq 1 $max_attempts); do
				if /tmp/nc-wrapper.sh "%s" %d; then
					exit 0
				fi
				sleep $sleep_interval
			done
		else
			# Fallback to multiple methods if wrapper doesn't exist
			for i in $(seq 1 $max_attempts); do
				# Try nc with -z flag (most common)
				if command -v nc >/dev/null 2>&1 && nc -z "%s" %d >/dev/null 2>&1; then
					exit 0
				fi
				
				# Try with echo pipe (works with most nc variants)
				if command -v nc >/dev/null 2>&1 && echo | nc "%s" %d >/dev/null 2>&1; then
					exit 0
				fi
				
				# Try with netcat if available
				if command -v netcat >/dev/null 2>&1; then
					if netcat -z "%s" %d >/dev/null 2>&1 || echo | netcat "%s" %d >/dev/null 2>&1; then
						exit 0
					fi
				fi
				
				sleep $sleep_interval
			done
		fi
		
		# If we get here, connection failed after all attempts
		echo "Failed to connect to %s:%d after $max_attempts attempts" >&2
		exit 1
	`, address, port, address, port, address, port, address, port, address, port)}
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
