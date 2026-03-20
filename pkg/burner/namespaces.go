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

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

// Cleanup resources specific to kube-burner with in a given list of namespaces
func CleanupNamespacesUsingGVR(ctx context.Context, ex JobExecutor, namespacesToDelete []string) error {
	labelSelector := fmt.Sprintf("%s=%s", config.KubeBurnerLabelJob, ex.Name)
	for _, namespace := range namespacesToDelete {
		log.Infof("Deleting namespace %s using GVR", namespace)
		for _, obj := range ex.objects {
			if obj.namespaced {
				// Collect GVRs to clean up: use static GVR if available, otherwise use dynamically resolved GVRs
				var gvrsToCleanup []schema.GroupVersionResource
				if obj.gvr != (schema.GroupVersionResource{}) {
					gvrsToCleanup = append(gvrsToCleanup, obj.gvr)
				} else {
					// Templated kind case: use dynamically resolved GVRs stored during creation
					obj.resolvedGVRs.Range(func(key, value any) bool {
						if gvr, ok := value.(schema.GroupVersionResource); ok {
							gvrsToCleanup = append(gvrsToCleanup, gvr)
						}
						return true
					})
				}
				for _, gvr := range gvrsToCleanup {
					cleanupNamespaceResourcesByGVR(ctx, ex, gvr, namespace, labelSelector)
				}
			}
		}
		err := waitForDeleteNamespacedResources(ctx, ex, namespace, labelSelector)
		if err != nil {
			return err
		}
	}
	return nil
}

// cleanupNamespaceResourcesByGVR cleans up resources for a specific GVR
func cleanupNamespaceResourcesByGVR(ctx context.Context, ex JobExecutor, gvr schema.GroupVersionResource, namespace string, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(gvr).Namespace(namespace)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Unable to list %vs in %v: %v", gvr.Resource, namespace, err)
		return
	}
	if len(resources.Items) > 0 {
		log.Infof("Deleting %d %ss labeled with %s in %s", len(resources.Items), gvr.Resource, labelSelector, namespace)
	}
	for _, item := range resources.Items {
		if err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationBackground)}); err != nil {
			if !errors.IsNotFound(err) {
				log.Errorf("Error deleting %v/%v in %v: %v", item.GetKind(), item.GetName(), namespace, err)
			}
		}
	}
}

// Deletes resources with the give labelSelector within a namespace
func CleanupNamespaceResourcesByLabel(ctx context.Context, ex JobExecutor, obj *object, namespace string, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(obj.gvr).Namespace(namespace)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Unable to list %vs in %v: %v", obj.Kind, namespace, err)
		return
	}
	if len(resources.Items) > 0 {
		log.Debugf("Deleting %d %ss labeled with %s in %s", len(resources.Items), obj.Kind, labelSelector, namespace)
	}
	for _, item := range resources.Items {
		if err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationBackground)}); err != nil {
			if !errors.IsNotFound(err) {
				log.Errorf("Error deleting %v/%v in %v: %v", item.GetKind(), item.GetName(), namespace, err)
			}
		}
	}
}

// Cleanup non-namespaced resources using executor list
func CleanupNonNamespacedResourcesByLabel(ctx context.Context, ex JobExecutor, object *object, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(object.gvr)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Debugf("Unable to list resources for object: %v error: %v. Hence skipping it", object.Object, err)
		return
	}
	if len(resources.Items) > 0 {
		log.Infof("Deleting %d %ss labeled with %s", len(resources.Items), object.Kind, labelSelector)
		util.DeleteNonNamespacedResources(ctx, resources, resourceInterface)
	}
}

// cleanupNonNamespacedResourcesByGVR cleans up non-namespaced resources for a specific GVR
func cleanupNonNamespacedResourcesByGVR(ctx context.Context, ex JobExecutor, gvr schema.GroupVersionResource, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(gvr)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Debugf("Unable to list resources for GVR %s: %v. Skipping", gvr.Resource, err)
		return
	}
	if len(resources.Items) > 0 {
		log.Infof("Deleting %d %ss labeled with %s", len(resources.Items), gvr.Resource, labelSelector)
		util.DeleteNonNamespacedResources(ctx, resources, resourceInterface)
	}
}

func waitForDeleteNamespacedResources(ctx context.Context, ex JobExecutor, namespace string, labelSelector string) error {
	for _, obj := range ex.objects {
		// If churning is enabled and object doesn't have churning enabled we skip that object from deletion
		if config.IsChurnEnabled(ex.Job) && !obj.Churn {
			continue
		}
		if obj.namespaced {
			// Collect GVRs to wait for: use static GVR if available, otherwise use dynamically resolved GVRs
			var gvrsToWait []schema.GroupVersionResource
			if obj.gvr != (schema.GroupVersionResource{}) {
				gvrsToWait = append(gvrsToWait, obj.gvr)
			} else {
				// Templated kind case: use dynamically resolved GVRs stored during creation
				obj.resolvedGVRs.Range(func(key, value any) bool {
					if gvr, ok := value.(schema.GroupVersionResource); ok {
						gvrsToWait = append(gvrsToWait, gvr)
					}
					return true
				})
			}
			for _, gvr := range gvrsToWait {
				err := waitForDeleteResourceByGVRInNamespace(ctx, ex, gvr, namespace, labelSelector)
				if err != nil {
					return fmt.Errorf("error waiting for %s to be deleted: %v", gvr.Resource, err)
				}
			}
		}
	}
	return nil
}

func waitForDeleteResourceByGVRInNamespace(ctx context.Context, ex JobExecutor, gvr schema.GroupVersionResource, namespace string, labelSelector string) error {
	resourceInterface := ex.dynamicClient.Resource(gvr).Namespace(namespace)
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		objList, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Errorf("Error listing objects: %v", err)
			return false, err
		}
		if len(objList.Items) > 0 {
			log.Debugf("Waiting for %d %ss labeled with %s in %s to be deleted", len(objList.Items), gvr.Resource, labelSelector, namespace)
			return false, nil
		}
		return true, nil
	})
	return err
}

func WaitForDeleteResourceInNamespace(ctx context.Context, ex JobExecutor, obj *object, namespace string, labelSelector string) error {
	resourceInterface := ex.dynamicClient.Resource(obj.gvr).Namespace(namespace)
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		objList, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Errorf("Error listing objects: %v", err)
			return false, err
		}
		if len(objList.Items) > 0 {
			log.Debugf("Waiting for %d %ss labeled with %s in %s to be deleted", len(objList.Items), obj.Kind, labelSelector, namespace)
			return false, nil
		}
		return true, nil
	})
	return err
}
