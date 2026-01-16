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
				CleanupNamespaceResourcesByLabel(ctx, ex, obj, namespace, labelSelector)
			}
		}
		err := waitForDeleteNamespacedResources(ctx, ex, namespace, labelSelector)
		if err != nil {
			return err
		}
	}
	return nil
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
		log.Infof("Deleting %d %ss labeled with %s in %s", len(resources.Items), obj.Kind, labelSelector, namespace)
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
