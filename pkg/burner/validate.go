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
	"strings"
	"text/template/parse"
	"time"

	"github.com/itchyny/gojq"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	authv1 "k8s.io/api/authorization/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"kubevirt.io/client-go/kubecli"
)

// Validate performs a dry-run validation of the workload.
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

		// Job-level configuration checks
		validateJobConfig(job, configSpec.GlobalConfig.Timeout, restCfg, &errs)

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
			// Merge in any per-object InputVars so user-defined template variables are also present during validation.
			for k, v := range o.InputVars {
				templateData[k] = v
			}

			// Check ALL undefined template variables in one pass via AST analysis.
			if !job.DefaultMissingKeysWithZero {
				checkUndefinedVars(raw, templateData, job.Name, o.ObjectTemplate, functionTemplates, &errs)
			}

			rendered, err := util.RenderTemplate(raw, templateData, util.MissingKeyZero, functionTemplates)
			if err != nil {
				errs = append(errs, fmt.Errorf("job %s, template %s: rendering error: %w", job.Name, o.ObjectTemplate, err))
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
				if mapper != nil {
					// Validate GVK against the live cluster.
					validateGVK(mapper, obj, job.Name, o.ObjectTemplate, &errs)
					// RBAC check for the verb implied by the job type.
					if clientSet != nil {
						validateRBAC(clientSet, mapper, obj, job, o.ObjectTemplate, &errs)
					}
				}
			}
		}
	}

	// Measurement-specific RBAC checks.
	if clientSet != nil {
		for _, measurement := range configSpec.GlobalConfig.Measurements {
			if measurement.NodeAffinity != nil {
				if err := checkResourceAccess(clientSet, "apps", "daemonsets", "create", types.PprofNamespace); err != nil {
					errs = append(errs, fmt.Errorf("measurement %s RBAC check failed: %w", measurement.Name, err))
				}
			}
		}
	}

	return errs
}

// validateJobConfig checks job configuration to ensure it is well-formed and supported by the burner, including execution mode, job type, QPS/Burst settings, and churn configuration.
// It also performs KubeVirt-specific checks for KubeVirt jobs.
func validateJobConfig(job config.Job, defaultTimeout time.Duration, restCfg *rest.Config, errs *[]error) {
	// Execution mode
	if job.ExecutionMode != "" &&
		job.ExecutionMode != config.ExecutionModeParallel &&
		job.ExecutionMode != config.ExecutionModeSequential {
		*errs = append(*errs, fmt.Errorf("job %s: unsupported executionMode %q (must be %q or %q)",
			job.Name, job.ExecutionMode, config.ExecutionModeParallel, config.ExecutionModeSequential))
	}

	// QPS/Burst validation
	verifyJobDefaults(&job, defaultTimeout)

	// Job type validation
	verifyJobType(&job, errs)

	// Churn config sanity
	if config.IsChurnEnabled(job) {
		if job.ChurnConfig.Percent < 0 || job.ChurnConfig.Percent > 100 {
			*errs = append(*errs, fmt.Errorf("job %s: churn percent %d is out of range (0-100)",
				job.Name, job.ChurnConfig.Percent))
		}
		if job.JobType != config.CreationJob {
			*errs = append(*errs, fmt.Errorf("job %s: churn is only supported for %q jobs, got %q",
				job.Name, config.CreationJob, job.JobType))
		}
	}

	// Wait condition field path validation
	for _, o := range job.Objects {
		for _, sp := range o.WaitOptions.CustomStatusPaths {
			if _, err := gojq.Parse(sp.Key); err != nil {
				*errs = append(*errs, fmt.Errorf("job %s, template %s: invalid jq path %q in customStatusPaths: %w",
					job.Name, o.ObjectTemplate, sp.Key, err))
			}
		}
	}

	// Patch job checks
	if job.JobType == config.PatchJob {
		for _, o := range job.Objects {
			if len(o.PatchType) == 0 {
				*errs = append(*errs, fmt.Errorf("job %s, template %s: empty patchType not allowed for patch jobs",
					job.Name, o.ObjectTemplate))
			} else if o.PatchType == string(apitypes.ApplyPatchType) && strings.HasSuffix(o.ObjectTemplate, "json") {
				*errs = append(*errs, fmt.Errorf("job %s, template %s: apply patch type requires YAML, not JSON",
					job.Name, o.ObjectTemplate))
			}
		}
	}

	// Delete/Patch jobs require labelSelector on each object
	if job.JobType == config.DeletionJob || job.JobType == config.PatchJob {
		for _, o := range job.Objects {
			if len(o.LabelSelector) == 0 {
				*errs = append(*errs, fmt.Errorf("job %s, object kind %q: empty labelSelector not allowed for %s jobs",
					job.Name, o.Kind, job.JobType))
			}
		}
	}

	// KubeVirt job checks
	if job.JobType == config.KubeVirtJob {
		validateKubeVirtJob(&job, restCfg, errs)
	}
}

// validateKubeVirtJob checks KubeVirt job configuration
func validateKubeVirtJob(job *config.Job, restCfg *rest.Config, errs *[]error) {
	// Only check kubevirt client connectivity when a cluster is reachable.
	if restCfg != nil {
		if _, err := kubecli.GetKubevirtClientFromRESTConfig(restCfg); err != nil {
			*errs = append(*errs, fmt.Errorf("job %s: failed to get kubevirt client: %w", job.Name, err))
		}
	}

	for _, o := range job.Objects {
		if len(o.KubeVirtOp) == 0 {
			*errs = append(*errs, fmt.Errorf("job %s: empty kubeVirtOp not allowed", job.Name))
		} else if _, ok := supportedOps[o.KubeVirtOp]; !ok {
			*errs = append(*errs, fmt.Errorf("job %s: unsupported kubeVirtOp %q", job.Name, o.KubeVirtOp))
		}
	}
}

// checkUndefinedVars parses the template AST to find ALL undefined variable
// references in one pass, rather than stopping at the first missing key.
func checkUndefinedVars(raw []byte, knownVars map[string]any, jobName, templateFile string, funcTemplates []string, errs *[]error) {
	// Stub functions to allow parsing field references.
	funcMap := map[string]any{}
	for _, ft := range funcTemplates {
		// ft may be a path; use its base name stripped of extension as a stub.
		funcMap[ft] = func() string { return "" }
	}

	trees, err := parse.Parse("tpl", string(raw), "", "", funcMap)
	if err != nil {
		// Parse errors are caught by the render step; skip here.
		return
	}

	// Collect all top-level field names used in the template.
	used := make(map[string]bool)
	for _, tree := range trees {
		collectFieldNames(tree.Root, used)
	}

	// Report every field that is not in knownVars.
	for field := range used {
		if _, ok := knownVars[field]; !ok {
			*errs = append(*errs, fmt.Errorf("job %s, template %s: undefined variable {{.%s}}",
				jobName, templateFile, field))
		}
	}
}

// collectFieldNames recursively walks a parse tree node and records every
// top-level .FieldName reference (i.e. .Foo, not .Foo.Bar sub-fields).
func collectFieldNames(node parse.Node, out map[string]bool) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *parse.FieldNode:
		if len(n.Ident) > 0 {
			out[n.Ident[0]] = true
		}
	case *parse.ListNode:
		for _, child := range n.Nodes {
			collectFieldNames(child, out)
		}
	case *parse.ActionNode:
		collectFieldNames(n.Pipe, out)
	case *parse.PipeNode:
		for _, cmd := range n.Cmds {
			collectFieldNames(cmd, out)
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			collectFieldNames(arg, out)
		}
	case *parse.IfNode:
		collectFieldNames(n.Pipe, out)
		collectFieldNames(n.List, out)
		collectFieldNames(n.ElseList, out)
	case *parse.RangeNode:
		collectFieldNames(n.Pipe, out)
		collectFieldNames(n.List, out)
		collectFieldNames(n.ElseList, out)
	case *parse.WithNode:
		collectFieldNames(n.Pipe, out)
		collectFieldNames(n.List, out)
		collectFieldNames(n.ElseList, out)
	case *parse.TemplateNode:
		collectFieldNames(n.Pipe, out)
	}
}

// verifyJobType checks job type configuration
func verifyJobType(job *config.Job, errs *[]error) {
	jobTypes := []config.JobType{
		config.CreationJob,
		config.DeletionJob,
		config.PatchJob,
		config.ReadJob,
		config.KubeVirtJob,
	}
	for _, jobType := range jobTypes {
		if job.JobType == jobType {
			return
		}
	}
	*errs = append(*errs, fmt.Errorf("job %s: unsupported jobType %q (must be %q, %q, %q, %q, or %q)",
		job.Name, job.JobType, config.CreationJob, config.DeletionJob, config.PatchJob, config.ReadJob, config.KubeVirtJob))
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

// jobTypeToVerb maps each JobType to the Kubernetes RBAC verb it requires.
func jobTypeToVerb(jt config.JobType) string {
	jobTypeSlice := []config.JobType{
		config.CreationJob,
		config.DeletionJob,
		config.PatchJob,
		config.ReadJob,
		config.KubeVirtJob,
	}
	for _, jobType := range jobTypeSlice {
		if jt == jobType {
			return string(jt)
		}
	}
	return ""
}

// validateRBAC performs a SelfSubjectAccessReview for the verb implied by the job type against the resource discovered from the object's GVK.
func validateRBAC(clientSet kubernetes.Interface, mapper apimeta.RESTMapper, obj *unstructured.Unstructured, job config.Job, templateFile string, errs *[]error) {
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		return
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		// GVK not found — already reported by validateGVK.
		return
	}
	gvr := mapping.Resource
	verb := jobTypeToVerb(job.JobType)
	namespace := job.Namespace
	if namespace == "" {
		namespace = "default"
	}
	if err := checkResourceAccess(clientSet, gvr.Group, gvr.Resource, verb, namespace); err != nil {
		*errs = append(*errs, fmt.Errorf("job %s, template %s: RBAC check failed for %s %s/%s: %w",
			job.Name, templateFile, verb, gvr.Group, gvr.Resource, err))
	} else {
		log.Debugf("  ✅ RBAC: %s %s/%s in namespace %s — allowed", verb, gvr.Group, gvr.Resource, namespace)
	}
}

// checkResourceAccess uses SelfSubjectAccessReview to verify that the current user / service account has the given verb on the specified resource.
func checkResourceAccess(clientSet kubernetes.Interface, group, resource, verb, namespace string) error {
	ssar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      verb,
				Group:     group,
				Resource:  resource,
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
		return fmt.Errorf("RBAC denied: cannot %s %s/%s in namespace %s (reason: %s)",
			verb, group, resource, namespace, response.Status.Reason)
	}
	return nil
}
