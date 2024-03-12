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
	// We use 50ms precision thanks to sleep 0.1
	cmd := []string{"bash", "-c", fmt.Sprintf("while true; do nc -w 0.1s -z %s %d && break; sleep 0.05; done", address, port)}
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
