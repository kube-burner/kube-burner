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
	"strings"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

func (ex *Executor) setupReadJob(configSpec config.Spec, mapper meta.RESTMapper) {
	log.Debugf("Preparing read job: %s", ex.Name)
	ex.itemHandler = readHandler
	if ex.ExecutionMode == "" {
		ex.ExecutionMode = config.ExecutionModeSequential
	}

	for _, o := range ex.Objects {
		log.Debugf("Job %s: %s %s with selector %s", ex.Name, ex.JobType, o.Kind, labels.Set(o.LabelSelector))
		ex.objects = append(ex.objects, newObject(o, configSpec, mapper, APIVersionV1))
	}
	log.Infof("Job %s: %d iterations", ex.Name, ex.JobIterations)
}

func readHandler(ex *Executor, obj *object, item unstructured.Unstructured, iteration int, objectTimeUTC int64, wg *sync.WaitGroup) {
	defer wg.Done()
	ex.limiter.Wait(context.TODO())
	kind := strings.ToLower(obj.Object.Kind)
	if ex.EnableWatcher {
		watcher := metrics.NewWatcher(
			ex.clientSet.CoreV1().RESTClient().(*rest.RESTClient),
			kind+"Watcher",
			kind+"s",
			corev1.NamespaceAll,
			func(options *metav1.ListOptions) {
				options.LabelSelector = labels.SelectorFromSet(obj.Object.LabelSelector).String()
			},
			nil,
		)
		watcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) {},
			UpdateFunc: func(oldObj, newObj interface{}) {},
			DeleteFunc: func(obj interface{}) {},
		})
		if err := watcher.StartAndCacheSync(); err != nil {
			log.Errorf("Watcher error: %s", err)
		}

		log.Infof("%s started, running for %s...", kind+"Watcher", ex.WatchDuration)
		time.Sleep(ex.WatchDuration)

		log.Infof("Stopping %s after %s...", kind+"Watcher", ex.WatchDuration)
		if err := watcher.StopWatcher(); err != nil {
			log.Errorf("failed to stop watcher: %v", err)
		}
		log.Infof("%s stopped successfully", kind+"Watcher")
	} else {
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
}
