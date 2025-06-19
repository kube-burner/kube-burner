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
	"fmt"
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
		plural := util.NaivePlural(kind)
		indexNum := strconv.Itoa(index)
		replicaNum := strconv.Itoa(replica)
		watcherName := kindLower + "_watcher_" + indexNum + "_replica_" + replicaNum
		restClient, err := util.ResourceToRESTClient(wm.clientSet, kindLower)
		if err != nil {
			wm.recordError(fmt.Errorf("error getting REST client for %s: %w", kindLower, err))
			return
		}
		watcher := NewWatcher(
			restClient,
			watcherName,
			plural,
			corev1.NamespaceAll,
			func(options *metav1.ListOptions) {
				options.LabelSelector = labels.SelectorFromSet(labelSelector).String()
			},
			nil,
		)

		if err := watcher.StartAndCacheSync(); err != nil {
			wm.recordError(fmt.Errorf("error starting %s: %w", watcherName, err))
			return
		}
		log.Infof("Started %s", watcherName)

		wm.mu.Lock()
		wm.watchers[watcherName] = watcher
		wm.mu.Unlock()
	}()
}

// Wait method to wait for all the watchers
func (wm *WatcherManager) Wait() []error {
	wm.wg.Wait()
	wm.errMu.Lock()
	defer wm.errMu.Unlock()
	return wm.errs
}

// StopAll method to stop all the watchers
func (wm *WatcherManager) StopAll() []error {
	var stopErrors []error
	wm.mu.Lock()
	defer wm.mu.Unlock()
	for name, watcher := range wm.watchers {
		log.Infof("Stopping %s", name)
		if err := watcher.StopWatcher(); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("failed to stop watcher %s: %v", name, err))
		}
	}
	return stopErrors
}

// recordError records the error
func (wm *WatcherManager) recordError(err error) {
	wm.errMu.Lock()
	defer wm.errMu.Unlock()
	wm.errs = append(wm.errs, err)
}
