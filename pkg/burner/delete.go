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
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
)

func setupDeleteJob(jobConfig config.Job) Executor {
	var ex Executor
	log.Debugf("Preparing delete job: %s", jobConfig.Name)
	mapper := newRESTMapper()
	for _, o := range jobConfig.Objects {
		if o.APIVersion == "" {
			o.APIVersion = "v1"
		}
		gvk := schema.FromAPIVersionAndKind(o.APIVersion, o.Kind)
		mapping, err := mapper.RESTMapping(gvk.GroupKind())
		if err != nil {
			log.Fatal(err)
		}
		if len(o.LabelSelector) == 0 {
			log.Fatalf("Empty labelSelectors not allowed with: %s", o.Kind)
		}
		obj := object{
			gvr:           mapping.Resource,
			labelSelector: o.LabelSelector,
		}
		obj.Namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
		log.Debugf("Job %s: Delete %s with selector %s", jobConfig.Name, gvk.Kind, labels.Set(obj.labelSelector))
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// RunDeleteJob executes a deletion job
func (ex *Executor) RunDeleteJob() {
	var wg sync.WaitGroup
	var itemList *unstructured.UnstructuredList
	for _, obj := range ex.objects {
		labelSelector := labels.Set(obj.labelSelector).String()
		listOptions := metav1.ListOptions{
			LabelSelector: labelSelector,
		}
		err := util.RetryWithExponentialBackOff(func() (done bool, err error) {
			itemList, err = DynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
			if err != nil {
				log.Errorf("Error found listing %s labeled with %s: %s", obj.gvr.Resource, labelSelector, err)
				return false, nil
			}
			return true, nil
		}, 1*time.Second, 3, 0, ex.MaxWaitTimeout)
		if err != nil {
			continue
		}
		log.Infof("Found %d %s with selector %s, removing them", len(itemList.Items), obj.gvr.Resource, labelSelector)
		for _, item := range itemList.Items {
			wg.Add(1)
			go func(item unstructured.Unstructured) {
				defer wg.Done()
				ex.limiter.Wait(context.TODO())
				var err error
				if obj.Namespaced {
					log.Debugf("Removing %s/%s from namespace %s", item.GetKind(), item.GetName(), item.GetNamespace())
					err = DynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
				} else {
					log.Debugf("Removing %s/%s", item.GetKind(), item.GetName())
					err = DynamicClient.Resource(obj.gvr).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
				}
				if err != nil {
					log.Errorf("Error found removing %s/%s: %s", item.GetKind(), item.GetName(), err)
				}
			}(item)
			if ex.JobIterationDelay > 0 {
				log.Infof("Sleeping for %v", ex.JobIterationDelay)
				time.Sleep(ex.JobIterationDelay)
			}
		}
		if ex.Job.WaitForDeletion {
			wait.PollUntilContextCancel(context.TODO(), 2*time.Second, true, func(ctx context.Context) (done bool, err error) {
				itemList, err = DynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
				if err != nil {
					log.Error(err.Error())
					return false, nil
				}
				if len(itemList.Items) > 0 {
					log.Debugf("Waiting for %d %s labeled with %s to be deleted", len(itemList.Items), obj.gvr.Resource, labelSelector)
					return false, nil
				}
				return true, nil
			})
		}
	}
}
