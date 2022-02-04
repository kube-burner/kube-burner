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
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubectl/pkg/scheme"
)

const (
	// Parameters for retrying with exponential backoff.
	retryBackoffInitialDuration = 1 * time.Second
	retryBackoffFactor          = 3
	retryBackoffJitter          = 0
	retryBackoffSteps           = 3
	objectLimit                 = 500
)

func prepareTemplate(original []byte) ([]byte, error) {
	// Removing all placeholders from template.
	// This needs to be done due to placeholders not being valid yaml
	if isEmpty(original) {
		return nil, fmt.Errorf("template is empty")
	}
	r, err := regexp.Compile(`\{\{.*\}\}`)
	if err != nil {
		return nil, fmt.Errorf("regexp creation error: %v", err)
	}
	original = r.ReplaceAll(original, []byte{})
	return original, nil
}

func yamlToUnstructured(y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvr, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatalf("Error decoding YAML: %s", err)
	}
	return o, gvr
}

// Cleanup deletes old namespaces from a given job
func (ex *Executor) Cleanup() {
	if ex.Config.Cleanup {
		CleanupNamespaces(ClientSet, ex.selector.ListOptions)
	}
}

// Verify verifies the number of created objects
func (ex *Executor) Verify() bool {
	var objList *unstructured.UnstructuredList
	var replicas int
	success := true
	log.Info("Verifying created objects")
	for objectIndex, obj := range ex.objects {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kube-burner-uuid=%s,kube-burner-job=%s,kube-burner-index=%d", ex.uuid, ex.Config.Name, objectIndex),
			Limit:         objectLimit,
		}
		err := RetryWithExponentialBackOff(func() (done bool, err error) {
			replicas = 0
			for {
				objList, err = dynamicClient.Resource(obj.gvr).Namespace(metav1.NamespaceAll).List(context.TODO(), listOptions)
				if err != nil {
					log.Errorf("Error verifying object: %s", err)
					return false, nil
				}
				replicas += len(objList.Items)
				listOptions.Continue = objList.GetContinue()
				// If continue is not set
				if listOptions.Continue == "" {
					break
				}
			}
			return true, nil
		})
		// Mark success to false if we found an error
		if err != nil {
			success = false
			continue
		}
		objectsExpected := ex.Config.JobIterations * obj.replicas
		if replicas != objectsExpected {
			log.Errorf("%s found: %d Expected: %d", obj.gvr.Resource, replicas, objectsExpected)
			success = false
		} else {
			log.Infof("%s found: %d Expected: %d", obj.gvr.Resource, replicas, objectsExpected)
		}
	}
	return success
}

// RetryWithExponentialBackOff a utility for retrying the given function with exponential backoff.
func RetryWithExponentialBackOff(fn wait.ConditionFunc) error {
	backoff := wait.Backoff{
		Duration: retryBackoffInitialDuration,
		Factor:   retryBackoffFactor,
		Jitter:   retryBackoffJitter,
		Steps:    retryBackoffSteps,
	}
	return wait.ExponentialBackoff(backoff, fn)
}

func isEmpty(raw []byte) bool {
	return strings.TrimSpace(string(raw)) == ""
}
