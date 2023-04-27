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

	log "github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/cloud-bulldozer/kube-burner/pkg/burner/types"
)

func (ex *Executor) waitForObjects(ns string) {
	var wg sync.WaitGroup
	for _, obj := range ex.objects {
		if obj.wait {
			wg.Add(1)
			switch obj.kind {
			case "Deployment":
				go waitForDeployments(ns, ex.Config.MaxWaitTimeout, &wg)
			case "ReplicaSet":
				go waitForRS(ns, ex.Config.MaxWaitTimeout, &wg)
			case "ReplicationController":
				go waitForRC(ns, ex.Config.MaxWaitTimeout, &wg)
			case "StatefulSet":
				go waitForStatefulSet(ns, ex.Config.MaxWaitTimeout, &wg)
			case "DaemonSet":
				go waitForDS(ns, ex.Config.MaxWaitTimeout, &wg)
			case "Pod":
				go waitForPod(ns, ex.Config.MaxWaitTimeout, &wg)
			case "Build", "BuildConfig":
				go waitForBuild(ns, ex.Config.MaxWaitTimeout, obj.replicas, &wg)
			case "VirtualMachine":
				go waitForVM(ns, ex.Config.MaxWaitTimeout, &wg)
			case "VirtualMachineInstance":
				go waitForVMI(ns, ex.Config.MaxWaitTimeout, &wg)
			case "VirtualMachineInstanceReplicaSet":
				go waitForVMIRS(ns, ex.Config.MaxWaitTimeout, &wg)
			case "Job":
				go waitForJob(ns, ex.Config.MaxWaitTimeout, &wg)
			case "PersistentVolumeClaim":
				go waitForPVC(ns, ex.Config.MaxWaitTimeout, &wg)
			default:
				wg.Done()
			}
		}
	}
	wg.Wait()
	log.Infof("Actions in namespace %v completed", ns)
}

func waitForDeployments(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	// TODO handle errors such as timeouts
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		deps, err := waitClientSet.AppsV1().Deployments(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, dep := range deps.Items {
			if *dep.Spec.Replicas != dep.Status.ReadyReplicas {
				log.Debugf("Waiting for replicas from deployments in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForRS(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		rss, err := waitClientSet.AppsV1().ReplicaSets(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, rs := range rss.Items {
			if *rs.Spec.Replicas != rs.Status.ReadyReplicas {
				log.Debugf("Waiting for replicas from replicaSets in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForStatefulSet(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		stss, err := waitClientSet.AppsV1().StatefulSets(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, sts := range stss.Items {
			if *sts.Spec.Replicas != sts.Status.ReadyReplicas {
				log.Debugf("Waiting for replicas from statefulSets in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForPVC(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		pvc, err := waitClientSet.CoreV1().PersistentVolumeClaims(ns).List(context.TODO(), metav1.ListOptions{FieldSelector: "status.phase!=Bound"})
		if err != nil {
			return false, err
		}
		return len(pvc.Items) == 0, nil
	})
}

func waitForRC(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		rcs, err := waitClientSet.CoreV1().ReplicationControllers(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, rc := range rcs.Items {
			if *rc.Spec.Replicas != rc.Status.ReadyReplicas {
				log.Debugf("Waiting for replicas from replicationControllers in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForDS(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		dss, err := waitClientSet.AppsV1().DaemonSets(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, ds := range dss.Items {
			if ds.Status.DesiredNumberScheduled != ds.Status.NumberReady {
				log.Debugf("Waiting for replicas from daemonsets in ns %s to be ready", ns)
				return false, nil
			}
		}
		return true, nil
	})
}

func waitForPod(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		pods, err := waitClientSet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{FieldSelector: "status.phase!=Running"})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	})
}

func waitForBuild(ns string, maxWaitTimeout time.Duration, expected int, wg *sync.WaitGroup) {
	defer wg.Done()
	buildStatus := []string{"New", "Pending", "Running"}
	var build types.UnstructuredContent
	gvr := schema.GroupVersionResource{
		Group:    types.OpenShiftBuildGroup,
		Version:  types.OpenShiftBuildAPIVersion,
		Resource: types.OpenShiftBuildResource,
	}
	wait.PollImmediate(1*time.Second, maxWaitTimeout, func() (bool, error) {
		builds, err := waitDynamicClient.Resource(gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForJob(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	gvr := schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
	}
	verifyCondition(gvr, ns, "Complete", maxWaitTimeout)
}

func verifyCondition(gvr schema.GroupVersionResource, ns, condition string, maxWaitTimeout time.Duration) {
	var uObj types.UnstructuredContent
	wait.PollImmediate(10*time.Second, maxWaitTimeout, func() (bool, error) {
		objs, err := waitDynamicClient.Resource(gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
	VERIFY:
		for _, obj := range objs.Items {
			jsonBuild, err := obj.MarshalJSON()
			if err != nil {
				log.Errorf("Error decoding object: %s", err)
				return false, err
			}
			_ = json.Unmarshal(jsonBuild, &uObj)
			for _, c := range uObj.Status.Conditions {
				if c.Status == "True" {
					if c.Type == condition {
						continue VERIFY
					}
				}
			}
			log.Debugf("Waiting for %s in ns %s to be ready", gvr.Resource, ns)
			return false, err
		}
		return true, nil
	})
}

func waitForVM(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	vmGVR := schema.GroupVersionResource{
		Group:    types.KubevirtGroup,
		Version:  types.KubevirtAPIVersion,
		Resource: types.VirtualMachineResource,
	}
	verifyCondition(vmGVR, ns, "Ready", maxWaitTimeout)
}

func waitForVMI(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	vmiGVR := schema.GroupVersionResource{
		Group:    types.KubevirtGroup,
		Version:  types.KubevirtAPIVersion,
		Resource: types.VirtualMachineInstanceResource,
	}
	verifyCondition(vmiGVR, ns, "Ready", maxWaitTimeout)
}

func waitForVMIRS(ns string, maxWaitTimeout time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	var rs types.UnstructuredContent
	vmiGVRRS := schema.GroupVersionResource{
		Group:    types.KubevirtGroup,
		Version:  types.KubevirtAPIVersion,
		Resource: types.VirtualMachineInstanceReplicaSetResource,
	}
	wait.PollImmediate(10*time.Second, maxWaitTimeout, func() (bool, error) {
		objs, err := waitDynamicClient.Resource(vmiGVRRS).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			log.Debugf("replicaSets error %v", err)
			return false, err
		}
		for _, obj := range objs.Items {
			jsonBuild, err := obj.MarshalJSON()
			if err != nil {
				log.Errorf("Error decoding Build object: %s", err)
				return false, err
			}
			_ = json.Unmarshal(jsonBuild, &rs)
			if rs.Spec.Replicas != rs.Status.ReadyReplicas {
				log.Debugf("Waiting for replicas from replicaSets in ns %s to be running", ns)
				return false, nil
			}
		}
		return true, nil
	})
}
