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
	
	// First try to source our custom PATH to include /tmp
	sourceCmd := []string{"sh", "-c", "source /tmp/.netcat_profile 2>/dev/null || true"}
	sourceCmdReq := lc.clientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(lc.Pod.Name).
		Namespace(lc.Pod.Namespace).
		SubResource("exec")
	sourceCmdReq.VersionedParams(&corev1.PodExecOptions{
		Container: types.SvcLatencyCheckerName,
		Command:   sourceCmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)
	
	// Try to run the source command but ignore errors
	exec, _ := remotecommand.NewSPDYExecutor(&lc.restConfig, "POST", sourceCmdReq.URL())
	if exec != nil {
		exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		})
	}
	
	// Clear buffers
	stdout.Reset()
	stderr.Reset()
	
	// Now use a much simpler, more reliable approach with our wrapper script
	// The wrapper handles all netcat variations and fallbacks internally
	cmd := []string{"sh", "-c", fmt.Sprintf(`
		# Add /tmp to PATH to find our wrappers
		export PATH=/tmp:$PATH
		
		# Maximum retry attempts
		max_attempts=200
		# Sleep interval between attempts (50ms)
		sleep_interval=0.05
		
		# Try the dedicated wrapper script first
		if [ -x "/tmp/nc-wrapper.sh" ]; then
			for i in $(seq 1 $max_attempts); do
				if /tmp/nc-wrapper.sh "%s" %d >/dev/null 2>&1; then
					exit 0
				fi
				sleep $sleep_interval
			done
		else
			# Fallback to trying all possible methods if wrapper doesn't exist
			for i in $(seq 1 $max_attempts); do
				# Try multiple netcat commands
				nc -z "%s" %d >/dev/null 2>&1 && exit 0
				echo | nc "%s" %d >/dev/null 2>&1 && exit 0
				netcat -z "%s" %d >/dev/null 2>&1 && exit 0
				echo | netcat "%s" %d >/dev/null 2>&1 && exit 0
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
