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
	"sync"

	"golang.org/x/time/rate"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type Watcher struct {
	name        string
	stopChannel chan struct{}
	Informer    cache.SharedIndexInformer
}

// WatcherManager type to manage watchers
type WatcherManager struct {
	clientSet kubernetes.Interface
	limiter   *rate.Limiter
	watchers  map[string]*Watcher
	mu        sync.Mutex
	wg        sync.WaitGroup
	errMu     sync.Mutex
	errs      []error
}
