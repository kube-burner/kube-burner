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
	"time"
	"context"
	"sync"
	"fmt"
    "strings"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"maps"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"kubevirt.io/client-go/kubecli"
)

// Executor contains the information required to execute a job
type ItemHandler func(ex *JobExecutor, obj *object, originalItem unstructured.Unstructured, iteration int, objectTimeUTC int64, wg *sync.WaitGroup)
type ObjectFinalizer func(ex *JobExecutor, obj *object)

type JobExecutor struct {
	config.Job
	objects           []*object
	uuid              string
	runid             string
	limiter           *rate.Limiter
	waitLimiter       *rate.Limiter
	nsRequired        bool
	itemHandler       ItemHandler
	objectFinalizer   ObjectFinalizer
	clientSet         kubernetes.Interface
	restConfig        *rest.Config
	dynamicClient     *dynamic.DynamicClient
	kubeVirtClient    kubecli.KubevirtClient
	functionTemplates []string
	embedCfg          *fileutils.EmbedConfiguration
}

func newExecutor(configSpec config.Spec, kubeClientProvider *config.KubeClientProvider, job config.Job, embedCfg *fileutils.EmbedConfiguration) JobExecutor {
	ex := JobExecutor{
		Job:               job,
		limiter:           rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
		uuid:              configSpec.GlobalConfig.UUID,
		runid:             configSpec.GlobalConfig.RUNID,
		waitLimiter:       rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
		functionTemplates: configSpec.GlobalConfig.FunctionTemplates,
		embedCfg:          embedCfg,
	}

	clientSet, runtimeRestConfig := kubeClientProvider.ClientSet(job.QPS, job.Burst)
	ex.clientSet = clientSet
	ex.restConfig = runtimeRestConfig
	ex.dynamicClient = dynamic.NewForConfigOrDie(ex.restConfig)

	_, setupRestConfig := kubeClientProvider.ClientSet(100, 100) // Hardcoded QPS/Burst
	mapper := newRESTMapper(discovery.NewDiscoveryClientForConfigOrDie(setupRestConfig))

	switch job.JobType {
	case config.CreationJob:
		ex.setupCreateJob(mapper)
	case config.DeletionJob:
		ex.setupDeleteJob(mapper)
	case config.PatchJob:
		ex.setupPatchJob(mapper)
	case config.ReadJob:
		ex.setupReadJob(mapper)
	case config.KubeVirtJob:
		ex.setupKubeVirtJob(mapper)
	default:
		log.Fatalf("Unknown jobType: %s", job.JobType)
	}
	return ex
}

func (ex *JobExecutor) renderTemplateForObject(obj *object, iteration, replicaIndex int, asJson bool) []byte {
	// Processing template
	templateData := map[string]any{
		jobName:      ex.Name,
		jobIteration: iteration,
		jobUUID:      ex.uuid,
		jobRunId:     ex.runid,
		replica:      replicaIndex,
	}
	maps.Copy(templateData, obj.InputVars)

	templateOption := util.MissingKeyError
	if ex.DefaultMissingKeysWithZero {
		templateOption = util.MissingKeyZero
	}

	renderedObj, err := util.RenderTemplate(obj.objectSpec, templateData, templateOption, ex.functionTemplates)
	if err != nil {
		log.Fatalf("Template error in %s: %s", obj.ObjectTemplate, err)
	}

	if asJson {
		newObject := &unstructured.Unstructured{}
		yamlToUnstructured(obj.ObjectTemplate, renderedObj, newObject)
		renderedObj, err = newObject.MarshalJSON()
		if err != nil {
			log.Fatalf("Error converting YAML to JSON")
		}
	}

	return renderedObj
}

func (ex *Executor) RunPreCreateNamespaces(ctx context.Context, iterationStart, iterationEnd int, nsLabels, nsAnnotations map[string]string, waitListNamespaces *[]string, namespacesCreated map[string]bool) error {
	if !ex.PreCreateNamespaces || !ex.nsRequired || !ex.NamespacedIterations {
		return nil
	}

	nsLabels = map[string]string{
		"kube-burner-job":   ex.Name,
		"kube-burner-uuid":  ex.uuid,
		"kube-burner-runid": ex.runid,
	}
	maps.Copy(nsLabels, ex.NamespaceLabels)

	for i := iterationStart; i < iterationEnd; i++ {
		ns := ex.generateNamespace(i)
		if namespacesCreated[ns] {
			continue
		}
		if err := util.CreateNamespace(ex.clientSet, ns, nsLabels, nil); err != nil {
			log.Errorf("Failed to create namespace %s: %v", ns, err)
			continue
		}
		log.Infof("Created namespace: %s", ns)

		namespacesCreated[ns] = true

		// Static wait (optional)
		if ex.NamespaceWaitDuration > 0 {
			log.Infof("Sleeping %v after creating namespace %s", ex.NamespaceWaitDuration, ns)
			time.Sleep(ex.NamespaceWaitDuration)
		}

		// Condition-based wait (optional)
		if ex.NamespaceWaitForCondition != nil {
			err := ex.waitForNamespaceCondition(ctx, ns, ex.NamespaceWaitForCondition.Resource, ex.NamespaceWaitForCondition.Timeout)
			if err != nil {
				log.Errorf("Namespace %s did not meet readiness condition: %v", ns, err)
			}
		}
	}
	return nil
}

func (ex *Executor) waitForNamespaceCondition(ctx context.Context, ns string, resource string, timeout time.Duration) error {
	log.Infof("Waiting for resource %s to appear in namespace %s (timeout: %s)", resource, ns, timeout)

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch strings.ToLower(resource) {
			case "networkpolicy":
				policies, err := ex.clientSet.NetworkingV1().NetworkPolicies(ns).List(ctx, metav1.ListOptions{})
				if err == nil && len(policies.Items) > 0 {
					log.Infof("Detected %d NetworkPolicy in namespace %s", len(policies.Items), ns)
					return nil
				}
			case "configmap":
				cms, err := ex.clientSet.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
				if err == nil && len(cms.Items) > 0 {
					log.Infof("Detected %d ConfigMap in namespace %s", len(cms.Items), ns)
					return nil
				}
			default:
				return fmt.Errorf("unsupported resource type: %s", resource)
			}

			if time.Since(start) > timeout {
				return fmt.Errorf("timeout reached waiting for %s in namespace %s", resource, ns)
			}
			time.Sleep(500 * time.Millisecond) // polling interval
	}
}

