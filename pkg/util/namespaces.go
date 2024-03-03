// Copyright 2023 The Kube-burner Authors.
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
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func CreateNamespace(clientSet kubernetes.Interface, name string, nsLabels map[string]string, nsAnnotations map[string]string) error {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: nsLabels, Annotations: nsAnnotations},
	}
	return RetryWithExponentialBackOff(func() (done bool, err error) {
		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
		if errors.IsForbidden(err) {
			log.Fatalf("authorization error creating namespace %s: %s", ns.Name, err)
			return false, err
		}
		if errors.IsAlreadyExists(err) {
			log.Infof("Namespace %s already exists", ns.Name)
			nsSpec, _ := clientSet.CoreV1().Namespaces().Get(context.TODO(), name, metav1.GetOptions{})
			if nsSpec.Status.Phase == corev1.NamespaceTerminating {
				log.Warnf("Namespace %s is in %v state, retrying", name, corev1.NamespaceTerminating)
				return false, nil
			}
			return true, nil
		} else if err != nil {
			log.Errorf("unexpected error creating namespace %s: %v", name, err)
			return false, nil
		}
		log.Debugf("Created namespace: %s", ns.Name)
		return true, err
	}, 5*time.Second, 3, 0, 5*time.Hour)
}

// CleanupNamespaces deletes namespaces with the given selector
func CleanupNamespaces(ctx context.Context, clientSet kubernetes.Interface, labelSelector string) {
	ns, err := clientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Error listing namespaces: %v", err.Error())
		return
	}
	if len(ns.Items) > 0 {
		log.Infof("Deleting %d namespaces with label: %s", len(ns.Items), labelSelector)
		for _, ns := range ns.Items {
			err := clientSet.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					log.Errorf("Error deleting namespace %s: %v", ns.Name, err)
				}
			}
		}
		waitForDeleteNamespaces(ctx, clientSet, labelSelector)
	}
}

func waitForDeleteNamespaces(ctx context.Context, clientSet kubernetes.Interface, labelSelector string) {
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		ns, err := clientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
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

// Cleanup non-namespaced resources with the given selector
func CleanupNonNamespacedResources(ctx context.Context, clientSet kubernetes.Interface, dynamicClient dynamic.Interface, labelSelector string) {
	serverResources, _ := clientSet.Discovery().ServerPreferredResources()
	log.Infof("Deleting non-namespace resources with label: %s", labelSelector)
	for _, resourceList := range serverResources {
		for _, resource := range resourceList.APIResources {
			if !resource.Namespaced {
				gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
				if err != nil {
					log.Errorf("Unable to scan the resource group version: %v", err)
				}
				resourceInterface := dynamicClient.Resource(schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: resource.Name,
				})
				resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
				if err != nil {
					log.Debugf("Unable to list resource %s: %v", resource.Name, err)
					continue
				}
				DeleteNonNamespacedResources(ctx, resources, resourceInterface)
			}
		}
	}
}

func DeleteNonNamespacedResources(ctx context.Context, resources *unstructured.UnstructuredList, resourceInterface dynamic.NamespaceableResourceInterface) {
	if len(resources.Items) > 0 {
		for _, item := range resources.Items {
			go func(item unstructured.Unstructured) {
				err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					log.Errorf("Error deleting %v/%v: %v", item.GetKind(), item.GetName(), err)
				}
			}(item)
		}
	}
}
