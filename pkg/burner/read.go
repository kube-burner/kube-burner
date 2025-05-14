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
)

func (ex *JobExecutor) setupReadJob(mapper meta.RESTMapper) {
	log.Debugf("Preparing read job: %s", ex.Name)
	ex.itemHandler = readHandler
	ex.ExecutionMode = config.ExecutionModeSequential

	for _, o := range ex.Objects {
		log.Debugf("Job %s: %s %s with selector %s", ex.Name, ex.JobType, o.Kind, labels.Set(o.LabelSelector))
		ex.objects = append(ex.objects, newObject(o, mapper, APIVersionV1, ex.embedCfg))
	}
	log.Infof("Job %s: %d iterations", ex.Name, ex.JobIterations)
}

func readHandler(ex *JobExecutor, obj *object, item unstructured.Unstructured, iteration int, objectTimeUTC int64, wg *sync.WaitGroup) {
	defer wg.Done()
	ex.limiter.Wait(context.TODO())
	var err error
	if obj.namespaced {
		log.Debugf("Reading %s/%s from namespace %s", item.GetKind(), item.GetName(), item.GetNamespace())
		_, err = ex.dynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Get(context.TODO(), item.GetName(), metav1.GetOptions{})
	} else {
		log.Debugf("Reading %s/%s", item.GetKind(), item.GetName())
		_, err = ex.dynamicClient.Resource(obj.gvr).Get(context.TODO(), item.GetName(), metav1.GetOptions{})
	}
	if err != nil {
		log.Errorf("Error found reading %s/%s: %s", item.GetKind(), item.GetName(), err)
	}
}
