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
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (ex *JobExecutor) setupDeleteJob(mapper meta.RESTMapper) {
	log.Debugf("Preparing delete job: %s", ex.Name)
	ex.itemHandler = deleteHandler
	if ex.WaitForDeletion {
		ex.objectFinalizer = verifyDelete
	}
	// Only a single iteration is supported for Delete job
	ex.JobIterations = 1
	// Use the sequential mode
	ex.ExecutionMode = config.ExecutionModeSequential
	// WaitWhenFinished expects the resources to exists. For this reason wait is handled by WaitForDeletion
	ex.WaitWhenFinished = false
	for _, o := range ex.Objects {
		log.Debugf("Job %s: %s %s with selector %s", ex.Name, ex.JobType, o.Kind, labels.Set(o.LabelSelector))
		ex.objects = append(ex.objects, newObject(o, mapper, APIVersionV1, ex.embedCfg))
	}
}

func deleteHandler(ex *JobExecutor, obj *object, item unstructured.Unstructured, iteration int, objectTimeUTC int64, wg *sync.WaitGroup) {
	defer wg.Done()
	ex.limiter.Wait(context.TODO())
	var err error
	if obj.namespaced {
		log.Debugf("Removing %s/%s from namespace %s", item.GetKind(), item.GetName(), item.GetNamespace())
		err = ex.dynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
	} else {
		log.Debugf("Removing %s/%s", item.GetKind(), item.GetName())
		err = ex.dynamicClient.Resource(obj.gvr).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
	}
	if err != nil {
		log.Errorf("Error found removing %s/%s: %s", item.GetKind(), item.GetName(), err)
	}
}

func verifyDelete(ex *JobExecutor, obj *object) {
	labelSelector := labels.Set(obj.LabelSelector).String()
	listOptions := metav1.ListOptions{
		LabelSelector: labelSelector,
	}
	wait.PollUntilContextCancel(context.TODO(), 2*time.Second, true, func(ctx context.Context) (done bool, err error) {
		itemList, err := ex.dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
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
