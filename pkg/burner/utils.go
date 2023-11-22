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
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/scheme"
)

const (
	objectLimit = 500
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

// Helps to set metadata labels
func setMetadataLabels(obj *unstructured.Unstructured, labels map[string]string) {
	// Will be useful for the resources like Deployments and Replicasets. Because
	// object.SetLabels(labels) doesn't actually set labels for the underlying
	// objects (i.e Pods under deployment/replicastes). So this function should help
	// us achieve that without breaking any of our labeling functionality.
	templatePath := []string{"spec", "template", "metadata", "labels"}
	metadata, found, _ := unstructured.NestedMap(obj.Object, templatePath...)
	if !found {
		return
	}
	for k, v := range labels {
		metadata[k] = v
	}
	unstructured.SetNestedMap(obj.Object, metadata, templatePath...)
}

func yamlToUnstructured(y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvk, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatalf("Error decoding YAML: %s", err)
	}
	return o, gvk
}

// Verify verifies the number of created objects
func (ex *Executor) Verify() bool {
	var objList *unstructured.UnstructuredList
	var replicas int
	success := true
	log.Info("Verifying created objects")
	for objectIndex, obj := range ex.objects {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kube-burner-uuid=%s,kube-burner-job=%s,kube-burner-index=%d", ex.uuid, ex.Name, objectIndex),
			Limit:         objectLimit,
		}
		err := util.RetryWithExponentialBackOff(func() (done bool, err error) {
			replicas = 0
			for {
				objList, err = DynamicClient.Resource(obj.gvr).Namespace(metav1.NamespaceAll).List(context.TODO(), listOptions)
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
		}, 1*time.Second, 3, 0, 1*time.Minute)
		// Mark success to false if we found an error
		if err != nil {
			success = false
			continue
		}
		objectsExpected := ex.JobIterations * obj.Replicas
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
func RetryWithExponentialBackOff(fn wait.ConditionFunc, duration time.Duration, factor, jitter float64, timeout time.Duration) error {
	steps := int(math.Ceil(math.Log(float64(timeout)/(float64(duration)*(1+jitter))) / math.Log(factor)))
	backoff := wait.Backoff{
		Duration: duration,
		Factor:   factor,
		Jitter:   jitter,
		Steps:    steps,
	}
	return wait.ExponentialBackoff(backoff, fn)
}

func isEmpty(raw []byte) bool {
	return strings.TrimSpace(string(raw)) == ""
}

// newMapper returns a discovery RESTMapper
func newRESTMapper() meta.RESTMapper {
	apiGroupResouces, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Fatal(err)
	}
	return restmapper.NewDiscoveryRESTMapper(apiGroupResouces)
}
