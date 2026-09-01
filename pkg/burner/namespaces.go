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
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				CleanupNamespacedResourcesByLabel(ctx, ex, obj, namespace, labelSelector)
			}
		}
		err := waitForDeleteNamespacedResources(ctx, ex, namespace, labelSelector)
		if err != nil {
			return err
		}
	}
	return nil
}

// Deletes resources with the given labelSelector within a namespace
func CleanupNamespacedResourcesByLabel(ctx context.Context, ex JobExecutor, obj *object, namespace string, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(obj.gvr).Namespace(namespace)
	err := resourceInterface.DeleteCollection(ctx,
		metav1.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationForeground)},
		metav1.ListOptions{LabelSelector: labelSelector},
	)
	if err != nil {
		log.Errorf("Error deleting %v labeled with %s: %v", obj.gvr.Resource, labelSelector, err)
	}
}

// Cleanup non-namespaced resources using executor list.
// Falls back to individual deletion when DeleteCollection is not supported (e.g. Namespaces).
func CleanupClusterScopedResourcesByLabel(ctx context.Context, ex JobExecutor, object *object, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(object.gvr)
	err := resourceInterface.DeleteCollection(ctx,
		metav1.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationForeground)},
		metav1.ListOptions{LabelSelector: labelSelector},
	)
	if err != nil {
		if errors.IsMethodNotSupported(err) {
			log.Debugf("DeleteCollection not supported for %v, falling back to individual deletion", object.gvr.Resource)
			deleteClusterScopedResourcesIndividually(ctx, ex, object, labelSelector)
			return
		}
		log.Errorf("Error deleting cluster-scoped %v labeled with %s: %v", object.gvr.Resource, labelSelector, err)
	}
}

func deleteClusterScopedResourcesIndividually(ctx context.Context, ex JobExecutor, object *object, labelSelector string) {
	resourceInterface := ex.dynamicClient.Resource(object.gvr)
	itemList, err := resourceInterface.List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		log.Errorf("Error listing cluster-scoped %v labeled with %s: %v", object.gvr.Resource, labelSelector, err)
		return
	}
	for _, item := range itemList.Items {
		ex.limiter.Wait(ctx)
		log.Debugf("Deleting cluster-scoped %v/%v", object.gvr.Resource, item.GetName())
		if err := resourceInterface.Delete(ctx, item.GetName(), metav1.DeleteOptions{
			PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
		}); err != nil && !errors.IsNotFound(err) {
			log.Errorf("Error deleting cluster-scoped %v/%v: %v", object.gvr.Resource, item.GetName(), err)
		}
	}
}

func waitForDeleteNamespacedResources(ctx context.Context, ex JobExecutor, namespace string, labelSelector string) error {
	for _, obj := range ex.objects {
		// If churning is enabled and object doesn't have churning enabled we skip that object from deletion
		if config.IsChurnEnabled(ex.Job) && !obj.Churn {
			continue
		}
		if obj.namespaced {
			err := waitForDeleteResourceInNamespace(ctx, ex, obj, namespace, labelSelector)
			if err != nil {
				return fmt.Errorf("error waiting for %s to be deleted: %v", obj.Kind, err)
			}
		}
	}
	return nil
}

func waitForDeleteResourceInNamespace(ctx context.Context, ex JobExecutor, obj *object, namespace string, labelSelector string) error {
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
