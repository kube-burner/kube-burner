// Copyright 2025 The Kube-burner Authors.
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
	"strconv"
	"strings"

	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// NewWatcherManager creates a new instance of watcher manager
func NewWatcherManager(clientSet kubernetes.Interface, limiter *rate.Limiter) *WatcherManager {
	return &WatcherManager{
		clientSet: clientSet,
		limiter:   limiter,
		watchers:  make(map[string]*Watcher),
	}
}

// Start method to start a watcher
func (wm *WatcherManager) Start(kind string, labelSelector map[string]string, index, replica int) {
	wm.wg.Add(1)
	go func() {
		defer wm.wg.Done()
		_ = wm.limiter.Wait(context.TODO())

		kindLower := strings.ToLower(kind)
		indexNum := strconv.Itoa(index)
		replicaNum := strconv.Itoa(replica)
		watcherName := kindLower + "_watcher_" + indexNum + "_replica_" + replicaNum
		restClient, err := util.ResourceToRESTClient(wm.clientSet, kindLower)
		if err != nil {
			log.Errorf("Error: %v", err)
		}
		watcher := NewWatcher(
			restClient,
			watcherName,
			kindLower+"s",
			corev1.NamespaceAll,
			func(options *metav1.ListOptions) {
				options.LabelSelector = labels.SelectorFromSet(labelSelector).String()
			},
			nil,
		)

		if err := watcher.StartAndCacheSync(); err != nil {
			log.Errorf("Watcher error: %s", err)
			return
		}
		log.Infof("Started %s", watcherName)

		wm.mu.Lock()
		wm.watchers[watcherName] = watcher
		wm.mu.Unlock()
	}()
}

// Wait method to wait for all the watchers
func (wm *WatcherManager) Wait() {
	wm.wg.Wait()
}

// StopWatcher method to stop a specific watcher
func (wm *WatcherManager) StopWatcher(name string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	watcher, exists := wm.watchers[name]
	if !exists {
		log.Warnf("Watcher %q not found, cannot stop", name)
		return
	}
	if err := watcher.StopWatcher(); err != nil {
		log.Errorf("Failed to stop watcher %q: %v", name, err)
		return
	}
	log.Infof("Watcher %q stopped successfully", name)
	delete(wm.watchers, name)
}

// StopAll method to stop all the watchers
func (wm *WatcherManager) StopAll() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for name, watcher := range wm.watchers {
		log.Infof("Stopping %s", name)
		if err := watcher.StopWatcher(); err != nil {
			log.Errorf("failed to stop watcher %s: %v", name, err)
		}
	}
}
