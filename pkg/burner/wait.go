// Copyright 2024 The Kube-burner Authors.
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

// Copyright 2024 The Kube-burner Authors.
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
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// waitForCRDEstablished waits for a CustomResourceDefinition to be established using the dynamic client
func waitForCRDEstablished(restConfig *rest.Config, crdName string, timeout time.Duration) error {
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}
	
	// CRD GVR for the dynamic client
	crdGVR := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	err = wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		unstructured, err := dynamicClient.Resource(crdGVR).Get(context.TODO(), crdName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		// Extract conditions from the unstructured object
		conditions, found, err := unstructured.NestedSlice(unstructured.Object, "status", "conditions")
		if err != nil || !found {
			log.Debugf("No conditions found yet for CRD %s", crdName)
			return false, nil
		}

		// Look for the Established condition with status True
		for _, c := range conditions {
			condition, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			
			if conditionType, found := condition["type"].(string); found && conditionType == "Established" {
				if status, found := condition["status"].(string); found && status == "True" {
					return true, nil
				}
			}
		}

		log.Debugf("Waiting for CRD %s to be established", crdName)
		return false, nil
	})

	return err
}
