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
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

func createNamespace(namespaceName string, nsLabels map[string]string) error {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespaceName, Labels: nsLabels},
	}
	return RetryWithExponentialBackOff(func() (done bool, err error) {
		_, err = ClientSet.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
		if errors.IsForbidden(err) {
			log.Fatalf("authorization error creating namespace %s: %s", ns.Name, err)
			return false, err
		}
		if errors.IsAlreadyExists(err) {
			log.Infof("Namespace %s already exists", ns.Name)
			nsSpec, _ := ClientSet.CoreV1().Namespaces().Get(context.TODO(), namespaceName, metav1.GetOptions{})
			if nsSpec.Status.Phase == corev1.NamespaceTerminating {
				log.Warnf("Namespace %s is in %v state, retrying", namespaceName, corev1.NamespaceTerminating)
				return false, nil
			}
			return true, nil
		} else if err != nil {
			log.Errorf("unexpected error creating namespace %s: %v", namespaceName, err)
			return false, nil
		}
		log.Debugf("Created namespace: %s", ns.Name)
		return true, err
	}, 5*time.Second, 3, 0, 5*time.Hour)
}

// CleanupNamespaces deletes namespaces with the given selector
func CleanupNamespaces(ctx context.Context, labelSelector string, cleanupWait bool) {
	ns, _ := ClientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if len(ns.Items) > 0 {
		log.Infof("Deleting namespaces with label %s", labelSelector)
		for _, ns := range ns.Items {
			err := ClientSet.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					log.Errorf("Error deleting namespace %s: %v", ns.Name, err)
				}
			}
		}
		if cleanupWait {
			waitForDeleteNamespaces(ctx, labelSelector)
		}
	}
}

// Cleanup resources specific to kube-burner with in a given list of namespaces
func CleanupNamespacesUsingGVR(ctx context.Context, ex Executor, namespacesToDelete []string) {
	for _, namespace := range namespacesToDelete {
		log.Infof("Deleting resources in namespace %s", namespace)
		labelSelector := fmt.Sprintf("kube-burner-job=%s", ex.Name)
		for _, obj := range ex.objects {
			CleanupNamespaceResourcesUsingGVR(ctx, obj, namespace, labelSelector)
		}
		waitForDeleteNamespacedResources(ctx, namespace, ex.objects, labelSelector)
	}
}

func CleanupNamespaceResourcesUsingGVR(ctx context.Context, obj object, namespace string, labelSelector string) {
	resourceInterface := DynamicClient.Resource(obj.gvr).Namespace(namespace)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	log.Infof("Deleting %ss labeled with %s in %s", obj.kind, labelSelector, namespace)
	if err != nil {
		log.Errorf("Unable to list %vs in %v: %v", obj.kind, namespace, err)
		return
	}
	for _, item := range resources.Items {
		if err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{}); err != nil {
			if !errors.IsNotFound(err) {
				log.Errorf("Error deleting %v/%v in %v: %v", item.GetKind(), item.GetName(), namespace, err)
			}
		}
	}
}

// Cleanup non-namespaced resources with the given selector
func CleanupNonNamespacedResources(ctx context.Context, labelSelector string) {
	serverResources, _ := ClientSet.Discovery().ServerPreferredResources()
	log.Infof("Deleting non-namespace resources with label %s", labelSelector)
	for _, resourceList := range serverResources {
		for _, resource := range resourceList.APIResources {
			if !resource.Namespaced {
				gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
				if err != nil {
					log.Errorf("Unable to scan the resource group version: %v", err)
				}
				resourceInterface := DynamicClient.Resource(schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: resource.Name,
				})
				resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
				if err != nil {
					log.Debugf("Unable to list resource: %s error: %v. Hence skipping it", resource.Name, err)
					continue
				}
				deleteNonNamespacedResources(ctx, resources, resourceInterface)
			}
		}
	}
}

// Cleanup non-namespaced resources using executor list
func CleanupNonNamespacedResourcesUsingGVR(ctx context.Context, object object, labelSelector string) {
	log.Infof("Deleting non-namespace %v with selector %v", object.kind, labelSelector)
	resourceInterface := DynamicClient.Resource(object.gvr)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Debugf("Unable to list resources for object: %v error: %v. Hence skipping it", object.Object, err)
		return
	}
	deleteNonNamespacedResources(ctx, resources, resourceInterface)
}

func deleteNonNamespacedResources(ctx context.Context, resources *unstructured.UnstructuredList, resourceInterface dynamic.NamespaceableResourceInterface) {
	if len(resources.Items) > 0 {
		for _, item := range resources.Items {
			go func(item unstructured.Unstructured) {
				err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					log.Errorf("Error deleting non-namespaced resources: %v", err)
				}
			}(item)
		}
	}
}

func waitForDeleteNamespaces(ctx context.Context, labelSelector string) {
	log.Info("Waiting for namespaces to be definitely deleted")
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		ns, err := ClientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			return false, err
		}
		if len(ns.Items) == 0 {
			return true, nil
		}
		log.Debugf("Waiting for %d namespaces labeled with %s to be deleted", len(ns.Items), labelSelector)
		return false, nil
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Fatalf("Timeout cleaning up namespaces: %v", err)
		}
		log.Errorf("Error cleaning up namespaces: %v", err)
	}
}

func waitForDeleteNamespacedResources(ctx context.Context, namespace string, objects []object, labelSelector string) {
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		allDeleted := true
		for _, obj := range objects {
			if obj.Namespaced {
				resourceInterface := DynamicClient.Resource(obj.gvr).Namespace(namespace)
				objList, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
				if err != nil {
					return false, err
				}
				if len(objList.Items) > 0 {
					allDeleted = false
					log.Debugf("Waiting for %d objects labeled with %s in %s to be deleted",
						len(objList.Items), labelSelector, namespace)
				}
			}
		}
		return allDeleted, nil
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Fatalf("Timeout waiting for objects to be deleted: %v", err)
		}
		log.Errorf("Error waiting for objects to be deleted: %v", err)
	}
}
