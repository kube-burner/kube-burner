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
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kube-burner/kube-burner/pkg/burner/types"
)

func (ex *Executor) waitForObjects(ns string, limiter *rate.Limiter) {
	for _, obj := range ex.objects {
		waitNs := ns
		if !obj.Wait {
			continue
		}
		// When the object has defined its own namespace, we use it
		// TODO objects with a fixed namespace don't need to be waited on a per iteration basis
		if obj.namespace != "" {
			waitNs = obj.namespace
		}
		if obj.WaitOptions.ForCondition != "" {
			if !obj.Namespaced {
				waitNs = ""
			}
			waitForCondition(obj.gvr, waitNs, obj.WaitOptions.ForCondition, ex.MaxWaitTimeout, limiter)
		} else {
			switch obj.kind {
			case "Deployment":
				waitForDeployments(waitNs, ex.MaxWaitTimeout, limiter)
			case "ReplicaSet":
				waitForRS(waitNs, ex.MaxWaitTimeout, limiter)
			case "ReplicationController":
				waitForRC(waitNs, ex.MaxWaitTimeout, limiter)
			case "StatefulSet":
				waitForStatefulSet(waitNs, ex.MaxWaitTimeout, limiter)
			case "DaemonSet":
				waitForDS(waitNs, ex.MaxWaitTimeout, limiter)
			case "Pod":
				waitForPod(waitNs, ex.MaxWaitTimeout, limiter)
			case "Build", "BuildConfig":
				waitForBuild(waitNs, ex.MaxWaitTimeout, obj.Replicas, limiter)
			case "VirtualMachine":
				waitForVM(waitNs, ex.MaxWaitTimeout, limiter)
			case "VirtualMachineInstance":
				waitForVMI(waitNs, ex.MaxWaitTimeout, limiter)
			case "VirtualMachineInstanceReplicaSet":
				waitForVMIRS(waitNs, ex.MaxWaitTimeout, limiter)
			case "Job":
				waitForJob(waitNs, ex.MaxWaitTimeout, limiter)
			case "PersistentVolumeClaim":
				waitForPVC(waitNs, ex.MaxWaitTimeout, limiter)
			}
		}
	}
	log.Infof("Actions in namespace %v completed", ns)
}

func waitForDeployments(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	// TODO handle errors such as timeouts
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		deps, err := ClientSet.AppsV1().Deployments(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForRS(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		rss, err := ClientSet.AppsV1().ReplicaSets(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForStatefulSet(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		stss, err := ClientSet.AppsV1().StatefulSets(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForPVC(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		pvc, err := ClientSet.CoreV1().PersistentVolumeClaims(ns).List(context.TODO(), metav1.ListOptions{FieldSelector: "status.phase!=Bound"})
		if err != nil {
			return false, err
		}
		return len(pvc.Items) == 0, nil
	})
}

func waitForRC(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		rcs, err := ClientSet.CoreV1().ReplicationControllers(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForDS(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		dss, err := ClientSet.AppsV1().DaemonSets(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForPod(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		pods, err := ClientSet.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{FieldSelector: "status.phase!=Running"})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	})
}

func waitForBuild(ns string, maxWaitTimeout time.Duration, expected int, limiter *rate.Limiter) {
	buildStatus := []string{"New", "Pending", "Running"}
	var build types.UnstructuredContent
	gvr := schema.GroupVersionResource{
		Group:    types.OpenShiftBuildGroup,
		Version:  types.OpenShiftBuildAPIVersion,
		Resource: types.OpenShiftBuildResource,
	}
	wait.PollUntilContextTimeout(context.TODO(), time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		builds, err := DynamicClient.Resource(gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
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

func waitForJob(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	gvr := schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
	}
	verifyCondition(gvr, ns, "Complete", maxWaitTimeout, limiter)
}

func waitForCondition(gvr schema.GroupVersionResource, ns, condition string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	verifyCondition(gvr, ns, condition, maxWaitTimeout, limiter)
}

func verifyCondition(gvr schema.GroupVersionResource, ns, condition string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	var uObj types.UnstructuredContent
	wait.PollUntilContextTimeout(context.TODO(), 10*time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		var objs *unstructured.UnstructuredList
		limiter.Wait(context.TODO())
		if ns != "" {
			objs, err = DynamicClient.Resource(gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		} else {
			objs, err = DynamicClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
		}
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
				if c.Status == "True" && c.Type == condition {
					continue VERIFY
				}
			}
			if ns != "" {
				log.Debugf("Waiting for %s in ns %s to be ready", gvr.Resource, ns)
			} else {
				log.Debugf("Waiting for %s to be ready", gvr.Resource)
			}
			return false, err
		}
		return true, nil
	})
}

func waitForVM(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	vmGVR := schema.GroupVersionResource{
		Group:    types.KubevirtGroup,
		Version:  types.KubevirtAPIVersion,
		Resource: types.VirtualMachineResource,
	}
	verifyCondition(vmGVR, ns, "Ready", maxWaitTimeout, limiter)
}

func waitForVMI(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	vmiGVR := schema.GroupVersionResource{
		Group:    types.KubevirtGroup,
		Version:  types.KubevirtAPIVersion,
		Resource: types.VirtualMachineInstanceResource,
	}
	verifyCondition(vmiGVR, ns, "Ready", maxWaitTimeout, limiter)
}

func waitForVMIRS(ns string, maxWaitTimeout time.Duration, limiter *rate.Limiter) {
	var rs types.UnstructuredContent
	vmiGVRRS := schema.GroupVersionResource{
		Group:    types.KubevirtGroup,
		Version:  types.KubevirtAPIVersion,
		Resource: types.VirtualMachineInstanceReplicaSetResource,
	}
	wait.PollUntilContextTimeout(context.TODO(), 10*time.Second, maxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		limiter.Wait(context.TODO())
		objs, err := DynamicClient.Resource(vmiGVRRS).Namespace(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			log.Debugf("VMIRS error %v", err)
			return false, err
		}
		for _, obj := range objs.Items {
			jsonBuild, err := obj.MarshalJSON()
			if err != nil {
				log.Errorf("Error decoding VMIRS object: %s", err)
				return false, err
			}
			_ = json.Unmarshal(jsonBuild, &rs)
			if rs.Spec.Replicas != rs.Status.ReadyReplicas {
				log.Debugf("Waiting for replicas from VMIRS in ns %s to be running", ns)
				return false, nil
			}
		}
		return true, nil
	})
}
