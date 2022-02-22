// Copyright 2022 The Kube-burner Authors.
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

package watcher

import (
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const informerTimeout = time.Minute

var (
	jobNamespace  string
	jobMetricName string
	jobUUID       string
	jobName       string
)

type Watcher struct {
	name        string
	stopChannel chan struct{}
	Informer    cache.SharedInformer

	// Metrics is a list of resource metrics
	Metrics map[string]interface{}

	// ResourceStatePerNS is the number of a given resource that are in a given condition per namespace
	// e.g., couting the number of pods in Ready state in a given namespace
	// the map is resource name: condition: namespace: counter
	ResourceStatePerNS map[string]map[string]map[string]float32

	mu sync.Mutex
}

func NewWatcher(restClient *rest.RESTClient, name string, resource string, namespace string) *Watcher {
	lw := cache.NewFilteredListWatchFromClient(
		restClient,
		resource,
		namespace,
		func(options *metav1.ListOptions) {},
	)

	return &Watcher{
		name:               name,
		stopChannel:        make(chan struct{}),
		Informer:           cache.NewSharedInformer(lw, nil, 0),
		Metrics:            make(map[string]interface{}),
		ResourceStatePerNS: make(map[string]map[string]map[string]float32),
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
	select {
	case <-p.stopChannel:
		return nil
	default:
		close(p.stopChannel)
	}
	return nil
}

// AddMetric inserts a metric in the Metric map
func (p *Watcher) AddMetric(id string, m interface{}) {
	p.Lock()
	p.Metrics[id] = m
	p.Unlock()
}

// GetMetric returns a metric from the Metric map
func (p *Watcher) GetMetric(id string) (interface{}, bool) {
	var m interface{}
	var exists bool
	p.Lock()
	m, exists = p.Metrics[id]
	p.Unlock()
	return m, exists
}

// GetMetrics returns the metric map
func (p *Watcher) GetMetrics() map[string]interface{} {
	p.Lock()
	m := p.Metrics
	p.Unlock()
	return m
}

// CleanMetrics reset metric map
func (p *Watcher) CleanMetrics() {
	p.Lock()
	p.Metrics = make(map[string]interface{})
	p.ResourceStatePerNS = make(map[string]map[string]map[string]float32)
	p.Unlock()
}

// AddResourceStatePerNS increments the counter from the ResourceStatePerNS map
func (p *Watcher) AddResourceStatePerNS(name string, condition string, namespace string, increment float32) {
	p.Lock()
	if _, exists := p.ResourceStatePerNS[name]; !exists {
		p.ResourceStatePerNS[name] = map[string]map[string]float32{}
	}
	if _, exists := p.ResourceStatePerNS[name][condition]; !exists {
		p.ResourceStatePerNS[name][condition] = map[string]float32{}
	}
	p.ResourceStatePerNS[name][condition][namespace] += increment
	p.Unlock()
}

// AddResourceStatePerNS returns the counter from the ResourceStatePerNS map
func (p *Watcher) GetResourceStatePerNSCount(name string, condition string, namespace string) int {
	var val int
	p.Lock()
	val = int(p.ResourceStatePerNS[name][condition][namespace])
	p.Unlock()
	return val
}

// Lock mutex to read or write in the maps
func (p *Watcher) Lock() {
	p.mu.Lock()
}

// Unlock mutex to read or write in the maps
func (p *Watcher) Unlock() {
	p.mu.Unlock()
}
