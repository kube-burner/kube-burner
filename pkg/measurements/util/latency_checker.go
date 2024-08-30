package util

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

func Ping(pod *corev1.Pod, clientSet kubernetes.Interface, restConfig rest.Config, addresses string, port int32, timeout time.Duration) (string, error) {
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := []string{"netpol", addresses}
	req := clientSet.CoreV1().RESTClient().Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: pod.Spec.Containers[0].Name,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		Command:   cmd,
		TTY:       true,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(&restConfig, "POST", req.URL())
	if err != nil {
		return addresses, err
	}
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return addresses, err
	}
	if ctx.Err() == context.DeadlineExceeded {
		return addresses, fmt.Errorf("timeout waiting for addresses %s:%d to be ready", addresses, port)
	}
	return strings.TrimSpace(stdout.String()), nil
}
