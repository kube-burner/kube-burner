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
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/cloud-bulldozer/kube-burner/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubectl/pkg/scheme"
)

func renderTemplate(original []byte, data interface{}) []byte {
	var rendered bytes.Buffer
	t, err := template.New("").Parse(string(original))
	if err != nil {
		log.Fatalf("Error parsing template: %s", err)
	}
	err = t.Execute(&rendered, data)
	if err != nil {
		log.Fatalf("Error rendering template: %s", err)
	}
	return rendered.Bytes()
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
		CleanupNamespaces(ClientSet, ex.selector)
	}
}

// Verify verifies the number of created objects
func (ex *Executor) Verify() bool {
	success := true
	log.Info("Verifying created objects")
	for objectIndex, obj := range ex.objects {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kube-burner-uuid=%s,kube-burner-job=%s,kube-burner-index=%d", ex.uuid, ex.Config.Name, objectIndex),
		}
		objList, err := dynamicClient.Resource(obj.gvr).Namespace(metav1.NamespaceAll).List(context.TODO(), listOptions)
		if err != nil {
			log.Errorf("Error verifying object: %s", err)
		}
		objectsExpected := ex.Config.JobIterations * obj.replicas
		if len(objList.Items) != objectsExpected {
			log.Errorf("%s found: %d Expected: %d", obj.gvr.Resource, len(objList.Items), objectsExpected)
			success = false
		} else {
			log.Infof("%s found: %d Expected: %d", obj.gvr.Resource, len(objList.Items), objectsExpected)
		}
	}
	return success
}
