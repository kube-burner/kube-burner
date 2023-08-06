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

	"github.com/openshift/client-go/config/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
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
		err := RetryWithExponentialBackOff(func() (done bool, err error) {
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
		}, 1*time.Second, 3, 0, 3)
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

// Verifies container registry and reports its status
func VerifyContainerRegistry(restConfig *rest.Config) bool {
	// Create an OpenShift client using the default configuration
	client, err := versioned.NewForConfig(restConfig)
	if err != nil {
		log.Error("Error connecting to the openshift cluster", err)
		return false
	}

	// Get the image registry object
	imageRegistry, err := client.ConfigV1().ClusterOperators().Get(context.TODO(), "image-registry", metav1.GetOptions{})
	if err != nil {
		log.Error("Error getting image registry object:", err)
		return false
	}

	// Check the status conditions
	logMessage := ""
	readyFlag := false
	for _, condition := range imageRegistry.Status.Conditions {
		if condition.Type == "Available" && condition.Status == "True" {
			readyFlag = true
			logMessage += " up and running"
		}
		if condition.Type == "Progressing" && condition.Status == "False" && condition.Reason == "Ready" {
			logMessage += " ready to use"
		}
		if condition.Type == "Degraded" && condition.Status == "False" && condition.Reason == "AsExpected" {
			logMessage += " with a healthy state"
		}
	}
	if readyFlag {
		log.Infof("Cluster image registry is%s", logMessage)
	} else {
		log.Info("Cluster image registry is not up and running")
	}
	return readyFlag
}

// RetryWithExponentialBackOff a utility for retrying the given function with exponential backoff.
func RetryWithExponentialBackOff(fn wait.ConditionFunc, duration time.Duration, factor, jitter float64, steps int) error {
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
