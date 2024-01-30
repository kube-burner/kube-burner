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

	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Cleanup resources specific to kube-burner with in a given list of namespaces
func CleanupNamespacesUsingGVR(ctx context.Context, ex Executor, namespacesToDelete []string) {
	for _, namespace := range namespacesToDelete {
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

// Cleanup non-namespaced resources using executor list
func CleanupNonNamespacedResourcesUsingGVR(ctx context.Context, object object, labelSelector string) {
	log.Infof("Deleting non-namespace %v with selector %v", object.kind, labelSelector)
	resourceInterface := DynamicClient.Resource(object.gvr)
	resources, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Debugf("Unable to list resources for object: %v error: %v. Hence skipping it", object.Object, err)
		return
	}
	util.DeleteNonNamespacedResources(ctx, resources, resourceInterface)
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
