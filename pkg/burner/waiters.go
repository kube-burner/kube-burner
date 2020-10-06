// Copyright 2020 The Kube-burner Authors.
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

package burner

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Build minimum build object
type Build struct {
	// Status represents the build status
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func (ex *Executor) waitForObjects(ns string) {
	waiting := false
	waitFor := true
	var wg sync.WaitGroup
	for _, obj := range ex.objects {
		if len(ex.Config.WaitFor) > 0 {
			waitFor = false
			for _, kind := range ex.Config.WaitFor {
				if obj.unstructured.GetKind() == kind {
					waitFor = true
					break
				}
			}
		}
		if waitFor {
			waiting = true
			wg.Add(1)
			switch obj.unstructured.GetKind() {
			case "Deployment":
				go waitForDeployments(ns, ex.Config.MaxWaitTimeout, &wg)
			case "ReplicaSet":
				go waitForRS(ns, ex.Config.MaxWaitTimeout, &wg)
			case "ReplicationController":
				go waitForRC(ns, ex.Config.MaxWaitTimeout, &wg)
			case "DaemonSet":
				go waitForDS(ns, ex.Config.MaxWaitTimeout, &wg)
			case "Pod":
				go waitForPod(ns, ex.Config.MaxWaitTimeout, &wg)
			case "Build":
				go waitForBuild(ns, ex.Config.MaxWaitTimeout, obj.replicas, &wg)
			case "BuildConfig":
				go waitForBuild(ns, ex.Config.MaxWaitTimeout, obj.replicas, &wg)
			default:
				wg.Done()
			}
		}
	}
	if waiting {
		log.Infof("Waiting %s for actions in namespace %v to be completed", ex.Config.MaxWaitTimeout, ns)
		wg.Wait()
	}
}

func waitForDeployments(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		deps, err := ClientSet.AppsV1().Deployments(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, dep := range deps.Items {
			if dep.Status.AvailableReplicas != *dep.Spec.Replicas {
				log.Debugf("Waiting for Deployments in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForRS(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		rss, err := ClientSet.AppsV1().ReplicaSets(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, rs := range rss.Items {
			if *rs.Spec.Replicas != rs.Status.AvailableReplicas {
				log.Debugf("Waiting for ReplicaSets in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForRC(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		rcs, err := ClientSet.CoreV1().ReplicationControllers(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, rc := range rcs.Items {
			if *rc.Spec.Replicas != rc.Status.ReadyReplicas {
				log.Debugf("Waiting for ReplicationControllers in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForDS(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		dss, err := ClientSet.AppsV1().DaemonSets(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, ds := range dss.Items {
			if ds.Status.DesiredNumberScheduled != ds.Status.NumberAvailable {
				log.Debugf("Waiting for daemonsets in ns %s to be readt", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForPod(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		pods, err := ClientSet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				log.Debugf("Waiting for pods in ns %s to be running", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForBuild(ns string, maxWaitTimeout time.Duration, expected int, wg *sync.WaitGroup) {
	defer wg.Done()
	buildStatus := []string{"New", "Pending", "Running"}
	var build Build
	gvr := schema.GroupVersionResource{
		Group:    "build.openshift.io",
		Version:  "v1",
		Resource: "builds",
	}
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		builds, err := dynamicClient.Resource(gvr).Namespace(ns).List(context.TODO(), v1.ListOptions{})
		if err != nil {
			return false, err
		}
		if len(builds.Items) < expected {
			log.Debugf("Waiting for Builds in ns %s to be completed", ns)
			return false, err
		}
		for _, b := range builds.Items {
			jsonBuild, err := b.MarshalJSON()
			if err != nil {
				log.Errorf("Error decoding Build object: %s", err)
			}
			_ = json.Unmarshal(jsonBuild, &build)
			for _, bs := range buildStatus {
				if build.Status.Phase == "" || build.Status.Phase == bs {
					log.Debugf("Waiting for Builds in ns %s to be completed", ns)
					return false, err
				}
			}
		}
		return true, nil
	})
}
