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

package metrics

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const informerTimeout = time.Minute

type Watcher struct {
	name        string
	stopChannel chan struct{}
	Informer    cache.SharedIndexInformer
}

// NewWatcher return a new ListWatcher of the specified resource and namespace
func NewWatcher(restClient *rest.RESTClient, name string, resource string, namespace string, optionsModifier func(options *metav1.ListOptions), indexers cache.Indexers) *Watcher {
	lw := cache.NewFilteredListWatchFromClient(
		restClient,
		resource,
		namespace,
		optionsModifier,
	)
	return &Watcher{
		name:        name,
		stopChannel: make(chan struct{}),
		Informer:    cache.NewSharedIndexInformer(lw, nil, 0, indexers),
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
