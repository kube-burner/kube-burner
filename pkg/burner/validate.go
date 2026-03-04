// Copyright 2026 The Kube-burner Authors.
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
	"io"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	authv1 "k8s.io/api/authorization/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Validate performs a dry-run validation of the workload. It checks every job's
// object templates for template rendering errors and structural problems.
func Validate(configSpec config.Spec, embedCfg *fileutils.EmbedConfiguration, restCfg *rest.Config) []error {
	var errs []error
	functionTemplates := configSpec.GlobalConfig.FunctionTemplates

	// Build cluster clients once if we have a live cluster connection.
	var mapper apimeta.RESTMapper
	var clientSet kubernetes.Interface
	if restCfg != nil {
		mapper = newRESTMapper(restCfg)
		if cs, err := kubernetes.NewForConfig(restCfg); err == nil {
			clientSet = cs
		} else {
			log.Debugf("Could not create clientSet for RBAC check: %v", err)
		}
	}

	for _, job := range configSpec.Jobs {
		log.Infof("🔍 Validating job: %s", job.Name)

		for _, o := range job.Objects {
			log.Debugf("  Checking template: %s", o.ObjectTemplate)

			// Read the template file
			f, err := fileutils.GetWorkloadReader(o.ObjectTemplate, embedCfg)
			if err != nil {
				errs = append(errs, fmt.Errorf("job %s: cannot read template %s: %w", job.Name, o.ObjectTemplate, err))
				continue
			}
			raw, err := io.ReadAll(f)
			if f.Close() != nil || err != nil {
				errs = append(errs, fmt.Errorf("job %s: cannot read template %s: %w", job.Name, o.ObjectTemplate, err))
				continue
			}

			// Render with sample data to catch Go-template errors
			templateData := map[string]any{
				jobName:      job.Name,
				jobIteration: 0,
				jobUUID:      configSpec.GlobalConfig.UUID,
				jobRunId:     configSpec.GlobalConfig.RUNID,
				replica:      1,
			}
			// Merge in any per-object InputVars so user-defined template variables
			// are also present during validation.
			for k, v := range o.InputVars {
				templateData[k] = v
			}

			templateOption := util.MissingKeyError
			if job.DefaultMissingKeysWithZero {
				templateOption = util.MissingKeyZero
			}

			rendered, err := util.RenderTemplate(raw, templateData, templateOption, functionTemplates)
			if err != nil {
				errs = append(errs, fmt.Errorf("job %s, template %s: %w", job.Name, o.ObjectTemplate, err))
				continue
			}

			// Decode rendered YAML unstructured
			objs, _ := yamlToUnstructuredMultiple(o.ObjectTemplate, rendered)

			if len(objs) == 0 {
				errs = append(errs, fmt.Errorf("job %s, template %s: no objects found", job.Name, o.ObjectTemplate))
				continue
			}

			for _, obj := range objs {
				validateUnstructured(obj, job.Name, o.ObjectTemplate, &errs)
				// Validate GVK against the live cluster when connected.
				if mapper != nil {
					validateGVK(mapper, obj, job.Name, o.ObjectTemplate, &errs)
				}
			}
		}
	}

	if clientSet != nil {
		for _, measurement := range configSpec.GlobalConfig.Measurements {
			if measurement.NodeAffinity != nil {
				if err := checkDaemonSetCreateAccess(clientSet, types.PprofNamespace); err != nil {
					errs = append(errs, fmt.Errorf("measurement %s RBAC check failed: %w", measurement.Name, err))
				}
			}
		}
	}

	return errs
}

// validateUnstructured checks that mandatory top-level fields are present.
func validateUnstructured(obj *unstructured.Unstructured, jobName, templateFile string, errs *[]error) {
	if obj == nil || len(obj.Object) == 0 {
		*errs = append(*errs, fmt.Errorf("job %s, template %s: object decoded to empty map", jobName, templateFile))
		return
	}
	if obj.GetKind() == "" {
		*errs = append(*errs, fmt.Errorf("job %s, template %s: object is missing 'kind'", jobName, templateFile))
	}
	if obj.GetAPIVersion() == "" {
		*errs = append(*errs, fmt.Errorf("job %s, template %s: object is missing 'apiVersion'", jobName, templateFile))
	}
}

// validateGVK checks that the object's GroupVersionKind is a known API resource in the target cluster via the discovery API.
func validateGVK(mapper apimeta.RESTMapper, obj *unstructured.Unstructured, jobName, templateFile string, errs *[]error) {
	gvk := obj.GroupVersionKind()
	// Skip if kind/version are missing — validateUnstructured already reported those.
	if gvk.Kind == "" || gvk.Version == "" {
		return
	}
	if _, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
		*errs = append(*errs, fmt.Errorf("job %s, template %s: kind %q (apiVersion=%s) is not registered in the cluster: %w",
			jobName, templateFile, gvk.Kind, gvk.GroupVersion().String(), err))
	}
}

// checkDaemonSetCreateAccess uses SelfSubjectAccessReview to verify that the
// current service account / user has permission to create DaemonSets in the
// given namespace.
func checkDaemonSetCreateAccess(clientSet kubernetes.Interface, namespace string) error {
	ssar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     "apps",
				Resource:  "daemonsets",
			},
		},
	}

	response, err := clientSet.AuthorizationV1().
		SelfSubjectAccessReviews().
		Create(context.Background(), ssar, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to perform access review: %w", err)
	}

	if !response.Status.Allowed {
		return fmt.Errorf("RBAC denied: cannot create daemonsets in namespace %s (reason: %s)",
			namespace, response.Status.Reason)
	}

	log.Infof("✅ RBAC check passed: daemonset creation allowed in namespace %s", namespace)
	return nil
}
