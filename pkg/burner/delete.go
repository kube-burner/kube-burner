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

	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/restmapper"
)

func setupDeleteJob(jobConfig *config.Job) Executor {
	var ex Executor
	log.Debugf("Preparing delete job: %s", jobConfig.Name)
	apiGroupResouces, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Fatal(err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(apiGroupResouces)
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
		log.Debugf("Job %s: Delete %s with selector %s", jobConfig.Name, gvk.Kind, labels.Set(obj.labelSelector))
		ex.objects = append(ex.objects, obj)
	}
	jobConfig.PreLoadImages = false
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
		err := RetryWithExponentialBackOff(func() (done bool, err error) {
			itemList, err = dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
			if err != nil {
				log.Errorf("Error found listing %s labeled with %s: %s", obj.gvr.Resource, labelSelector, err)
				return false, nil
			}
			return true, nil
		}, 1*time.Second, 3, 0, 3)
		if err != nil {
			continue
		}
		log.Infof("Found %d %s with selector %s, removing them", len(itemList.Items), obj.gvr.Resource, labelSelector)
		for _, item := range itemList.Items {
			wg.Add(1)
			go func(item unstructured.Unstructured) {
				defer wg.Done()
				ex.limiter.Wait(context.TODO())
				err := dynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					log.Errorf("Error found removing %s %s: %s", item.GetKind(), item.GetName(), err)
				} else {
					ns := item.GetNamespace()
					if ns != "" {
						log.Debugf("Removing %s/%s from namespace %s", item.GetKind(), item.GetName(), ns)
					} else {
						log.Debugf("Removing %s/%s", item.GetKind(), item.GetName())
					}
				}
			}(item)
		}
		if ex.Job.WaitForDeletion {
			wait.PollUntilContextCancel(context.TODO(), 2*time.Second, true, func(ctx context.Context) (done bool, err error) {
				itemList, err = dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
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
