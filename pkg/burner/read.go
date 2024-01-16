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

	"github.com/kube-burner/kube-burner/pkg/config"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func setupReadJob(jobConfig config.Job) Executor {
	var ex Executor
	log.Debugf("Preparing read job: %s", jobConfig.Name)
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
		log.Debugf("Job %s: Read %s with selector %s", jobConfig.Name, gvk.Kind, labels.Set(obj.labelSelector))
		ex.objects = append(ex.objects, obj)
	}
	log.Infof("Job %s: %d iterations", jobConfig.Name, jobConfig.JobIterations)
	return ex
}

// RunReadJob executes a reading job
func (ex *Executor) RunReadJob(iterationStart, iterationEnd int) {
	var itemList *unstructured.UnstructuredList

	// We have to sum 1 since the iterations start from 1
	iterationProgress := (iterationEnd - iterationStart) / 10
	percent := 1
	for i := iterationStart; i < iterationEnd; i++ {
		if i == iterationStart+iterationProgress*percent {
			log.Infof("%v/%v iterations completed", i-iterationStart, iterationEnd-iterationStart)
			percent++
		}
		log.Debugf("Reading object from iteration %d", i)
		for _, obj := range ex.objects {
			labelSelector := labels.Set(obj.labelSelector).String()
			listOptions := metav1.ListOptions{
				LabelSelector: labelSelector,
			}
			err := RetryWithExponentialBackOff(func() (done bool, err error) {
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
			log.Infof("Found %d %s with selector %s, reading them", len(itemList.Items), obj.gvr.Resource, labelSelector)
			for _, item := range itemList.Items {
				go func(item unstructured.Unstructured) {
					ex.limiter.Wait(context.TODO())
					var err error
					if obj.Namespaced {
						log.Debugf("Reading %s/%s from namespace %s", item.GetKind(), item.GetName(), item.GetNamespace())
						_, err = DynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Get(context.TODO(), item.GetName(), metav1.GetOptions{})
					} else {
						log.Debugf("Reading %s/%s", item.GetKind(), item.GetName())
						_, err = DynamicClient.Resource(obj.gvr).Get(context.TODO(), item.GetName(), metav1.GetOptions{})
					}
					if err != nil {
						log.Errorf("Error found reading %s/%s: %s", item.GetKind(), item.GetName(), err)
					}
				}(item)
				if ex.JobIterationDelay > 0 {
					log.Infof("Sleeping for %v", ex.JobIterationDelay)
					time.Sleep(ex.JobIterationDelay)
				}
			}
		}
	}
}
