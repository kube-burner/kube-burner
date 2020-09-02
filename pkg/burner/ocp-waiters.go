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
	"encoding/json"
	"sync"
	"time"

	"github.com/rsevilla87/kube-burner/log"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Build minimum build object
type Build struct {
	// Status represents the build status
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func waitForBuild(ns string, wg *sync.WaitGroup, obj object) {
	defer wg.Done()
	buildStatus := []string{"New", "Pending", "Running"}
	var build Build
	err := wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
		builds, err := dynamicClient.Resource(obj.gvr).Namespace(ns).List(context.TODO(), v1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, b := range builds.Items {
			jsonBuild, err := b.MarshalJSON()
			if err != nil {
				log.Errorf("Error decoding Build object: %s", err)
			}
			_ = json.Unmarshal(jsonBuild, &build)
			for _, bs := range buildStatus {
				if build.Status.Phase == bs {
					log.Debugf("Waiting for Builds in ns %s to be completed", ns)
					return false, err
				}
			}
		}
		return true, nil
	})
	if err != nil {
		log.Errorf("Error waiting for builds: %s", err)
	}
}
