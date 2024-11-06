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

	"github.com/itchyny/gojq"
	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kube-burner/kube-burner/pkg/burner/types"
)

func (ex *Executor) waitForObjects(ns string) {
	var err error
	for _, obj := range ex.objects {
		if !obj.Wait || obj.ready {
			continue
		}
		// When the object has defined its own namespace, we use it
		if obj.namespace != "" {
			ns = obj.namespace
		}
		if obj.WaitOptions.ForCondition != "" {
			ex.verifyCondition(ns, obj)
		} else {
			kind := obj.kind
			if obj.WaitOptions.Kind != "" {
				kind = obj.WaitOptions.Kind
				ns = corev1.NamespaceAll
			}
			switch kind {
			case Deployment, ReplicaSet, ReplicationController, StatefulSet, DaemonSet, VirtualMachineInstanceReplicaSet:
				err = ex.waitForReplicas(ns, obj, waitStatusMap[kind])
			case Pod:
				err = ex.waitForPod(ns, obj)
			case Build, BuildConfig:
				err = ex.waitForBuild(ns, obj)
			case VirtualMachine, VirtualMachineInstance:
				err = ex.waitForVMorVMI(ns, obj)
			case Job:
				err = ex.waitForJob(ns, obj)
			case PersistentVolumeClaim:
				err = ex.waitForPVC(ns, obj)
			}
		}
		if err != nil {
			log.Fatalf("Error waiting for objects in namespace %s: %v", ns, err)
		}
		if obj.namespace != "" || obj.RunOnce {
			obj.ready = true
		}
	}
	if ns != "" {
		log.Infof("Actions in namespace %v completed", ns)
	} else {
		log.Info("Actions completed")
	}
}

func (ex *Executor) waitForReplicas(ns string, obj object, waitPath statusPath) error {
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, ex.MaxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		ex.waitLimiter.Wait(context.TODO())
		resources, err := DynamicClient.Resource(obj.gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set(obj.WaitOptions.LabelSelector).String(),
		})
		if err != nil {
			log.Errorf("Error listing %s in %s: %v", obj.kind, ns, err)
			return false, nil
		}
		for _, resource := range resources.Items {
			replicas, _, err := unstructured.NestedFieldCopy(resource.Object, waitPath.expectedReplicasPath...)
			if err != nil {
				return false, err
			}
			readyReplicas, _, err := unstructured.NestedFieldCopy(resource.Object, waitPath.readyReplicasPath...)
			if err != nil {
				return false, err
			}
			if replicas != readyReplicas {
				log.Debugf("Waiting for replicas from %s in ns %s to be ready", obj.kind, ns)
				return false, nil
			}
		}
		return true, nil
	})
	return err
}

func (ex *Executor) waitForPVC(ns string, obj object) error {
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, ex.MaxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		ex.limiter.Wait(context.TODO())
		pvcs, err := ex.clientSet.CoreV1().PersistentVolumeClaims(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set(obj.WaitOptions.LabelSelector).String(),
		})
		if err != nil {
			log.Errorf("Error listing PVCs in %s: %v", ns, err)
			return false, nil
		}
		for _, pvc := range pvcs.Items {
			if pvc.Status.Phase != corev1.ClaimBound {
				log.Debugf("Waiting for pvcs in ns %s to be Bound", ns)
				return false, nil
			}
		}
		return true, nil
	})
	return err
}

func (ex *Executor) waitForPod(ns string, obj object) error {
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, ex.MaxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		// We need to paginate these requests to ensure we don't miss any pods
		listOptions := metav1.ListOptions{
			Limit:         1000,
			LabelSelector: labels.Set(obj.WaitOptions.LabelSelector).String(),
		}
		for {
			ex.limiter.Wait(context.TODO())
			pods, err := ex.clientSet.CoreV1().Pods(ns).List(context.TODO(), listOptions)
			if err != nil {
				log.Errorf("Error listing pods in %s: %v", ns, err)
				return false, nil
			}
			listOptions.Continue = pods.GetContinue()
			for _, pod := range pods.Items {
				if pod.Status.Phase != corev1.PodRunning {
					return false, nil
				}
				for _, c := range pod.Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionFalse {
						return false, nil
					}
				}
			}
			if listOptions.Continue == "" {
				break
			}
		}
		return true, nil
	})
	return err
}

func (ex *Executor) waitForBuild(ns string, obj object) error {
	buildStatus := []string{"New", "Pending", "Running"}
	var build types.UnstructuredContent
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, ex.MaxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		ex.limiter.Wait(context.TODO())
		builds, err := DynamicClient.Resource(obj.gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set(obj.WaitOptions.LabelSelector).String(),
		})
		if err != nil {
			log.Errorf("Error listing Builds in %s: %v", ns, err)
			return false, nil
		}
		if len(builds.Items) < obj.Replicas {
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
	return err
}

func (ex *Executor) waitForJob(ns string, obj object) error {
	if obj.WaitOptions.ForCondition == "" {
		obj.WaitOptions.ForCondition = "Complete"
	}
	return ex.verifyCondition(ns, obj)
}

func (ex *Executor) verifyCondition(ns string, obj object) error {
	var uObj types.UnstructuredContent
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, ex.MaxWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		var objs *unstructured.UnstructuredList
		ex.limiter.Wait(context.TODO())
		if obj.Namespaced {
			objs, err = DynamicClient.Resource(obj.gvr).Namespace(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: labels.Set(obj.WaitOptions.LabelSelector).String(),
			})
		} else {
			objs, err = DynamicClient.Resource(obj.gvr).List(context.TODO(), metav1.ListOptions{
				LabelSelector: labels.Set(obj.WaitOptions.LabelSelector).String(),
			})
		}
		if err != nil {
			if ns != "" {
				log.Errorf("Error listing %s in %s: %v", obj.kind, ns, err)
			} else {
				log.Errorf("Error listing %s: %v", obj.kind, err)
			}
			return false, nil
		}
	VERIFY:
		for _, item := range objs.Items {
			if obj.WaitOptions.CustomStatusPath != "" {
				status, found, err := unstructured.NestedMap(item.Object, "status")
				if err != nil || !found {
					log.Errorf("Error extracting or finding status in object %s/%s: %v", item.GetKind(), item.GetName(), err)
					return false, err
				}
				if len(status) != 0 {
					// Compile and execute the jq query
					query, err := gojq.Parse(obj.WaitOptions.CustomStatusPath)
					if err != nil {
						log.Errorf("Error parsing jq path: %s", obj.WaitOptions.CustomStatusPath)
						return false, err
					}
					iter := query.Run(status)
					for {
						v, ok := iter.Next()
						if !ok {
							break
						}
						if err, ok := v.(error); ok {
							log.Errorf("Error evaluating jq path: %s", err)
							return false, err
						}
						if v == obj.WaitOptions.ForCondition {
							continue VERIFY
						}
					}
				}
			} else {
				jsonBuild, err := item.MarshalJSON()
				if err != nil {
					log.Errorf("Error decoding object: %s", err)
					return false, err
				}
				_ = json.Unmarshal(jsonBuild, &uObj)
				for _, c := range uObj.Status.Conditions {
					if c.Status == "True" && c.Type == obj.WaitOptions.ForCondition {
						continue VERIFY
					}
				}
			}
			if obj.Namespaced {
				log.Debugf("Waiting for %s in ns %s to be ready", obj.gvr.Resource, ns)
			} else {
				log.Debugf("Waiting for %s to be ready", obj.gvr.Resource)
			}
			return false, err
		}
		return true, nil
	})
	return err
}

func (ex *Executor) waitForVMorVMI(ns string, obj object) error {
	if obj.WaitOptions.ForCondition == "" {
		obj.WaitOptions.ForCondition = "Ready"
	}
	return ex.verifyCondition(ns, obj)
}
