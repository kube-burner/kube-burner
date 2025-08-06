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

package watchers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

const informerTimeout = time.Minute

// NewWatcher return a new ListWatcher of the specified resource and namespace
func NewWatcher(
	dynamicClient dynamic.Interface,
	name string,
	gvr schema.GroupVersionResource,
	namespace string,
	optionsModifier func(*metav1.ListOptions),
	indexers cache.Indexers,
) *Watcher {
	resourceNamespaceable := dynamicClient.Resource(gvr)

	var resourceInterface dynamic.ResourceInterface
	if namespace == corev1.NamespaceAll {
		resourceInterface = resourceNamespaceable
	} else {
		resourceInterface = resourceNamespaceable.Namespace(namespace)
	}

	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			if optionsModifier != nil {
				optionsModifier(&opts)
			}
			return resourceInterface.List(context.TODO(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			if optionsModifier != nil {
				optionsModifier(&opts)
			}
			return resourceInterface.Watch(context.TODO(), opts)
		},
	}

	return &Watcher{
		name:        name,
		stopChannel: make(chan struct{}),
		Informer:    cache.NewSharedIndexInformer(lw, &unstructured.Unstructured{}, 0, indexers),
	}
}

// StartAndCacheSync starts informer and waits for the cache be synced.
func (p *Watcher) StartAndCacheSync() error {
	go p.Informer.Run(p.stopChannel)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerTimeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, p.Informer.HasSynced) {
		return fmt.Errorf("%s: Timed out waiting for caches to sync", p.name)
	}
	return nil
}

// StopWatcher stops Watcher measurement
func (p *Watcher) StopWatcher() error {
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerTimeout, func() {
		close(timeoutCh)
	})
	if !cache.WaitForCacheSync(timeoutCh, p.Informer.HasSynced) {
		return fmt.Errorf("%s: Timed out waiting for caches to sync", p.name)
	}
	timeoutTimer.Stop()
	close(p.stopChannel)
	return nil
}
