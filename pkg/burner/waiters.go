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
	"sync"
	"time"

	"github.com/rsevilla87/kube-burner/log"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func waitForDeployments(ns string, wg *sync.WaitGroup, obj object) {
	defer wg.Done()
	wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
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

func waitForRS(ns string, wg *sync.WaitGroup, obj object) {
	defer wg.Done()
	wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
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

func waitForRC(ns string, wg *sync.WaitGroup, obj object) {
	defer wg.Done()
	wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
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

func waitForDS(ns string, wg *sync.WaitGroup, obj object) {
	defer wg.Done()
	wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
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

func waitForPod(ns string, wg *sync.WaitGroup, obj object) {
	defer wg.Done()
	wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
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
