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
	}, 5*time.Second, 3, 0, 8)
}

// CleanupNamespaces deletes namespaces with the given selector
func CleanupNamespaces(ctx context.Context, l metav1.ListOptions, cleanupWait bool) {
	ns, _ := ClientSet.CoreV1().Namespaces().List(ctx, l)
	if len(ns.Items) > 0 {
		log.Infof("Deleting namespaces with label %s", l.LabelSelector)
		for _, ns := range ns.Items {
			err := ClientSet.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				log.Debugf("Namespace %s not found", ns.Name)
				continue
			}
			if err != nil {
				log.Errorf("Error cleaning up namespaces: %v", err)
			}
		}
		if cleanupWait {
			waitForDeleteNamespaces(ctx, l)
		}
	}
}

// Cleanup non-namespaced resources with the given selector
func CleanupNonNamespacedResources(ctx context.Context, l metav1.ListOptions, cleanupWait bool) {
	serverResources, _ := ClientSet.Discovery().ServerPreferredResources()
	log.Infof("Deleting non-namespace resources with label %s", l.LabelSelector)
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
				resources, err := resourceInterface.List(ctx, l)
				if err != nil {
					log.Debugf("Unable to list resource: %s error: %v. Hence skipping it", resource.Name, err)
					continue
				}
				if len(resources.Items) > 0 {
					for _, item := range resources.Items {
						go func(item unstructured.Unstructured) {
							err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{})
							if err != nil {
								log.Errorf("Error deleting non-namespaced resources: %v", err)
							}
						}(item)
					}
					if cleanupWait {
						waitForDeleteNonNamespacedResources(ctx, resourceInterface, l)
					}
				}
			}
		}
	}
}

func waitForDeleteNamespaces(ctx context.Context, l metav1.ListOptions) {
	log.Info("Waiting for namespaces to be definitely deleted")
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		ns, err := ClientSet.CoreV1().Namespaces().List(ctx, l)
		if err != nil {
			return false, err
		}
		if len(ns.Items) == 0 {
			return true, nil
		}
		log.Debugf("Waiting for %d namespaces labeled with %s to be deleted", len(ns.Items), l.LabelSelector)
		return false, nil
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Fatalf("Timeout cleaning up namespaces: %v", err)
		} else {
			log.Errorf("Error cleaning up namespaces: %v", err)
		}
	}
}

func waitForDeleteNonNamespacedResources(ctx context.Context, resourceInterface dynamic.NamespaceableResourceInterface, l metav1.ListOptions) {
	log.Info("Waiting for non-namespaced resources to be definitely deleted")
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		resources, err := resourceInterface.List(ctx, l)
		if err != nil {
			return false, err
		}
		if len(resources.Items) == 0 {
			return true, nil
		}
		log.Debugf("Waiting for %d %s labeled with %s to be deleted", len(resources.Items), resources.Items[0].GetKind(), l.LabelSelector)
		return false, nil
	})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Fatalf("Timeout cleaning up non-namespaced resources: %v", err)
		} else {
			log.Errorf("Error cleaning up non-namespaced resources: %v", err)
		}
	}
}
