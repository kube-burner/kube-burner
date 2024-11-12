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

	"github.com/kube-burner/kube-burner/pkg/config"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func setupReadJob(jobConfig config.Job) Executor {
	log.Debugf("Preparing read job: %s", jobConfig.Name)
	ex := Executor{
		Job:         jobConfig,
		itemHandler: readHandler,
	}
	ex.ExecutionMode = config.ExecutionModeSequential
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
			Object: o,
			gvr:    mapping.Resource,
		}
		obj.Namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
		log.Debugf("Job %s: Read %s with selector %s", jobConfig.Name, gvk.Kind, labels.Set(obj.LabelSelector))
		ex.objects = append(ex.objects, obj)
	}
	log.Infof("Job %s: %d iterations", jobConfig.Name, jobConfig.JobIterations)
	return ex
}

func readHandler(ex *Executor, obj object, item unstructured.Unstructured, iteration int, wg *sync.WaitGroup) {
	defer wg.Done()
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
}
