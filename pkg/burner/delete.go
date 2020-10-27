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

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func setupDeleteJob(jobConfig config.Job) Executor {
	log.Infof("Preparing delete job: %s", jobConfig.Name)
	var ex Executor
	for _, o := range jobConfig.Objects {
		if o.APIVersion == "" {
			o.APIVersion = "v1"
		}
		gvk := schema.FromAPIVersionAndKind(o.APIVersion, o.Kind)
		gvr, _ := meta.UnsafeGuessKindToResource(gvk)
		if o.LabelSelector == nil {
			log.Fatalf("Empty labelSelectors not allowed with: %s", o.Kind)
		}
		obj := object{
			gvr:           gvr,
			labelSelector: o.LabelSelector,
		}
		log.Infof("Job %s: Delete %s with selector %s", jobConfig.Name, gvk.Kind, labels.Set(obj.labelSelector))
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// RunDeleteJob executes a deletion job
func (ex *Executor) RunDeleteJob() {
	log.Infof("Triggering job: %s", ex.Config.Name)
	ex.Start = time.Now().UTC()
	var wg sync.WaitGroup
	var err error
	RestConfig, err = config.GetRestConfig(ex.Config.QPS, ex.Config.Burst)
	if err != nil {
		log.Fatalf("Error creating restConfig for kube-burner: %s", err)
	}
	ClientSet = kubernetes.NewForConfigOrDie(RestConfig)
	dynamicClient, err = dynamic.NewForConfig(RestConfig)
	if err != nil {
		log.Fatalf("Error creating DynamicClient: %s", err)
	}
	for _, obj := range ex.objects {
		labelSelector := labels.Set(obj.labelSelector).String()
		listOptions := metav1.ListOptions{
			LabelSelector: labelSelector,
		}
		resp, err := dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
		if err != nil {
			log.Errorf("Error found listing %s labeled with %s: %s", obj.gvr.Resource, labelSelector, err)
		}
		for _, item := range resp.Items {
			wg.Add(1)
			go func(item unstructured.Unstructured) {
				defer wg.Done()
				ex.limiter.Wait(context.TODO())
				err := dynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					log.Errorf("Error found removing %s %s from ns %s: %s", item.GetKind(), item.GetName(), item.GetNamespace(), err)
				} else {
					log.Infof("Removing %s %s from ns %s", item.GetKind(), item.GetName(), item.GetNamespace())
				}
			}(item)
		}
		if ex.Config.WaitForDeletion {
			wg.Wait()
			wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
				resp, err := dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
				if err != nil {
					return false, err
				}
				if len(resp.Items) > 0 {
					log.Infof("Waiting for %d %s labeled with %s to be deleted", len(resp.Items), obj.gvr.Resource, labelSelector)
					return false, nil
				}
				return true, nil
			})
		}
	}
	ex.End = time.Now().UTC()
}
