// Copyright 2026 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	etcdNamespace       = "openshift-etcd"
	etcdOperatorNS      = "openshift-etcd-operator"
	etcdCRName          = "cluster"
	defragCheckInterval = 10 * time.Second
	defragTimeout       = 5 * time.Minute
	memberGapDelay      = 30 * time.Second
)

// EtcdDefragManager manages etcd defragmentation operations
type EtcdDefragManager struct {
	clientSet     kubernetes.Interface
	dynamicClient dynamic.Interface
	restConfig    *rest.Config
}

// NewEtcdDefragManager creates a new EtcdDefragManager
func NewEtcdDefragManager(clientSet kubernetes.Interface, restConfig *rest.Config) *EtcdDefragManager {
	return &EtcdDefragManager{
		clientSet:     clientSet,
		dynamicClient: dynamic.NewForConfigOrDie(restConfig),
		restConfig:    restConfig,
	}
}

// DisableAutoDefrag disables automatic etcd defragmentation via the etcd operator
func (m *EtcdDefragManager) DisableAutoDefrag(ctx context.Context) error {
	log.Info("🔧 Disabling automatic etcd defragmentation")

	etcdGVR := schema.GroupVersionResource{
		Group:    "operator.openshift.io",
		Version:  "v1",
		Resource: "etcds",
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"unsupportedConfigOverrides": map[string]interface{}{
				"useUnsupportedUnsafeNonHANonProductionUnstableEtcd": true,
				"disableAutomaticDefragmentation":                    true,
			},
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = m.dynamicClient.Resource(etcdGVR).Patch(ctx, etcdCRName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to disable auto-defrag: %w", err)
	}

	log.Info("✅ Automatic etcd defragmentation disabled")
	return nil
}

// EnableAutoDefrag re-enables automatic etcd defragmentation
func (m *EtcdDefragManager) EnableAutoDefrag(ctx context.Context) error {
	log.Info("🔧 Re-enabling automatic etcd defragmentation")

	etcdGVR := schema.GroupVersionResource{
		Group:    "operator.openshift.io",
		Version:  "v1",
		Resource: "etcds",
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"unsupportedConfigOverrides": nil,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = m.dynamicClient.Resource(etcdGVR).Patch(ctx, etcdCRName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to enable auto-defrag: %w", err)
	}

	log.Info("✅ Automatic etcd defragmentation re-enabled")
	return nil
}

// RunDefragOnAllMembers runs defragmentation on all etcd members sequentially
func (m *EtcdDefragManager) RunDefragOnAllMembers(ctx context.Context) error {
	log.Info("🔧 Starting manual etcd defragmentation on all members")

	pods, err := m.clientSet.CoreV1().Pods(etcdNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=etcd",
	})
	if err != nil {
		return fmt.Errorf("failed to list etcd pods: %w", err)
	}

	if len(pods.Items) == 0 {
		log.Warn("No etcd pods found, skipping defragmentation")
		return nil
	}

	for i, pod := range pods.Items {
		if !strings.HasPrefix(pod.Name, "etcd-") {
			continue
		}

		log.Infof("🔧 Running defragmentation on %s (%d/%d)", pod.Name, i+1, len(pods.Items))

		if err := m.defragMember(ctx, pod.Name); err != nil {
			log.Warnf("Defragmentation failed on %s: %v", pod.Name, err)
			// Continue with other members
		} else {
			log.Infof("✅ Defragmentation completed on %s", pod.Name)
		}

		// Wait between members to allow cluster to stabilize
		if i < len(pods.Items)-1 {
			log.Infof("⏳ Waiting %v before next member", memberGapDelay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(memberGapDelay):
			}
		}
	}

	log.Info("✅ Manual etcd defragmentation completed on all members")
	return nil
}

// defragMember runs defragmentation on a single etcd member
func (m *EtcdDefragManager) defragMember(ctx context.Context, podName string) error {
	cmd := []string{
		"etcdctl",
		"defrag",
		"--command-timeout=60s",
	}

	stdout, stderr, err := m.execInPod(ctx, etcdNamespace, podName, "etcd", cmd)
	if err != nil {
		return fmt.Errorf("defrag command failed: %w, stderr: %s", err, stderr)
	}

	log.Debugf("Defrag output for %s: %s", podName, stdout)
	return nil
}

// execInPod executes a command in a pod and returns stdout, stderr
func (m *EtcdDefragManager) execInPod(ctx context.Context, namespace, podName, container string, cmd []string) (string, string, error) {
	req := m.clientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		Param("container", container).
		Param("stdout", "true").
		Param("stderr", "true")

	for _, c := range cmd {
		req.Param("command", c)
	}

	exec, err := remotecommand.NewSPDYExecutor(m.restConfig, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	return stdout.String(), stderr.String(), err
}

// CheckEtcdHealth checks if all etcd members are healthy
func (m *EtcdDefragManager) CheckEtcdHealth(ctx context.Context) error {
	log.Info("🏥 Checking etcd cluster health")

	// Check etcd ClusterOperator status
	etcdCOGVR := schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}

	co, err := m.dynamicClient.Resource(etcdCOGVR).Get(ctx, "etcd", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get etcd ClusterOperator: %w", err)
	}

	conditions, found, err := unstructured.NestedSlice(co.Object, "status", "conditions")
	if err != nil || !found {
		return fmt.Errorf("failed to get etcd ClusterOperator conditions")
	}

	for _, c := range conditions {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condition, "type")
		status, _, _ := unstructured.NestedString(condition, "status")

		switch condType {
		case "Available":
			if status != "True" {
				return fmt.Errorf("etcd ClusterOperator is not Available")
			}
		case "Degraded":
			if status == "True" {
				message, _, _ := unstructured.NestedString(condition, "message")
				return fmt.Errorf("etcd ClusterOperator is Degraded: %s", message)
			}
		}
	}

	log.Info("✅ Etcd cluster is healthy")
	return nil
}

// WaitForEtcdHealthy waits until etcd cluster becomes healthy
func (m *EtcdDefragManager) WaitForEtcdHealthy(ctx context.Context, timeout time.Duration) error {
	log.Infof("⏳ Waiting for etcd cluster to become healthy (timeout: %v)", timeout)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := m.CheckEtcdHealth(ctx); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(defragCheckInterval):
		}
	}

	return fmt.Errorf("timeout waiting for etcd to become healthy")
}

// DefragAndWaitHealthy performs defrag and waits for cluster to be healthy
func (m *EtcdDefragManager) DefragAndWaitHealthy(ctx context.Context) error {
	// Run defrag on all members
	if err := m.RunDefragOnAllMembers(ctx); err != nil {
		return fmt.Errorf("defragmentation failed: %w", err)
	}

	// Wait for cluster to be healthy
	if err := m.WaitForEtcdHealthy(ctx, defragTimeout); err != nil {
		return fmt.Errorf("etcd health check failed after defrag: %w", err)
	}

	return nil
}
