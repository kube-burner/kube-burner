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

package burner

import (
	"context"
	"io"
	"strings"
	"sync"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func setupPatchJob(jobConfig config.Job) Executor {
	var f io.Reader
	var err error
	log.Debugf("Preparing patch job: %s", jobConfig.Name)

	ex := Executor{
		Job:         jobConfig,
		itemHandler: patchHandler,
	}

	if len(ex.ExecutionMode) == 0 {
		ex.ExecutionMode = config.ExecutionModeParallel
	}
	if _, ok := supportedExecutionMode[ex.ExecutionMode]; !ok {
		log.Fatalf("Unsupported Execution Mode: %s", ex.ExecutionMode)
	}

	mapper := newRESTMapper()
	for _, o := range jobConfig.Objects {
		if o.APIVersion == "" {
			o.APIVersion = "v1"
		}
		log.Debugf("Rendering template: %s", o.ObjectTemplate)
		f, err = util.ReadConfig(o.ObjectTemplate)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		t, err := io.ReadAll(f)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}

		// Unlike create, we don't want to create the gvk with the entire template,
		// because it would try to use the properties of the patch data to find
		// the objects to patch.
		gvk := schema.FromAPIVersionAndKind(o.APIVersion, o.Kind)
		mapping, err := mapper.RESTMapping(gvk.GroupKind())
		if err != nil {
			log.Fatal(err)
		}
		if len(o.LabelSelector) == 0 {
			log.Fatalf("Empty labelSelectors not allowed with: %s", o.Kind)
		}
		if len(o.PatchType) == 0 {
			log.Fatalln("Empty Patch Type not allowed")
		}
		obj := object{
			gvr:        mapping.Resource,
			objectSpec: t,
			Object:     o,
		}
		obj.Namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
		log.Infof("Job %s: Patch %s with selector %s", jobConfig.Name, gvk.Kind, labels.Set(obj.LabelSelector))
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

func patchHandler(ex *Executor, obj object, originalItem unstructured.Unstructured, iteration int, wg *sync.WaitGroup) {
	defer wg.Done()
	// There are several patch modes. Three of them are client-side, and one
	// of them is server-side.
	var data []byte
	patchOptions := metav1.PatchOptions{}

	if strings.HasSuffix(obj.ObjectTemplate, "json") {
		if obj.PatchType == string(types.ApplyPatchType) {
			log.Fatalf("Apply patch type requires YAML")
		}
		data = obj.objectSpec
	} else {
		// Processing template
		templateData := map[string]interface{}{
			jobName:      ex.Name,
			jobIteration: iteration,
			jobUUID:      ex.uuid,
		}
		for k, v := range obj.InputVars {
			templateData[k] = v
		}

		templateOption := util.MissingKeyError
		if ex.DefaultMissingKeysWithZero {
			templateOption = util.MissingKeyZero
		}

		renderedObj, err := util.RenderTemplate(obj.objectSpec, templateData, templateOption)
		if err != nil {
			log.Fatalf("Template error in %s: %s", obj.ObjectTemplate, err)
		}

		// Converting to JSON if patch type is not Apply
		if obj.PatchType == string(types.ApplyPatchType) {
			data = renderedObj
			patchOptions.FieldManager = "kube-controller-manager"
		} else {
			newObject := &unstructured.Unstructured{}
			yamlToUnstructured(obj.ObjectTemplate, renderedObj, newObject)
			data, err = newObject.MarshalJSON()
			if err != nil {
				log.Errorf("Error converting patch to JSON")
			}
		}
	}

	ns := originalItem.GetNamespace()
	log.Debugf("Patching %s/%s in namespace %s", originalItem.GetKind(),
		originalItem.GetName(), ns)
	ex.limiter.Wait(context.TODO())

	var uns *unstructured.Unstructured
	var err error
	if obj.Namespaced {
		uns, err = DynamicClient.Resource(obj.gvr).Namespace(ns).
			Patch(context.TODO(), originalItem.GetName(),
				types.PatchType(obj.PatchType), data, patchOptions)
	} else {
		uns, err = DynamicClient.Resource(obj.gvr).
			Patch(context.TODO(), originalItem.GetName(),
				types.PatchType(obj.PatchType), data, patchOptions)
	}
	if err != nil {
		if errors.IsForbidden(err) {
			log.Fatalf("Authorization error patching %s/%s: %s", originalItem.GetKind(), originalItem.GetName(), err)
		} else {
			log.Errorf("Error patching object %s/%s in namespace %s: %s", originalItem.GetKind(),
				originalItem.GetName(), ns, err)
		}
	} else {
		log.Debugf("Patched %s/%s in namespace %s", uns.GetKind(), uns.GetName(), ns)
	}
}
