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
	"sync"

	"maps"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"kubevirt.io/client-go/kubecli"
)

// Executor contains the information required to execute a job
type ItemHandler func(ex *JobExecutor, obj *object, originalItem unstructured.Unstructured, iteration int, objectTimeUTC int64, wg *sync.WaitGroup)
type ObjectFinalizer func(ex *JobExecutor, obj *object)

type JobExecutor struct {
	config.Job
	objects           []*object
	createdNamespaces map[string]bool
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
	mapper            *restmapper.DeferredDiscoveryRESTMapper
	deletionStrategy  string
	objectOperations  int32
	nsChurning        bool
	backgroundHooks   []*hookProcess
	hookManager       *HookManager
}

func newExecutor(configSpec config.Spec, kubeClientProvider *config.KubeClientProvider, job config.Job, embedCfg *fileutils.EmbedConfiguration) JobExecutor {
	ex := JobExecutor{
		Job:               job,
		createdNamespaces: make(map[string]bool),
		limiter:           rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
		uuid:              configSpec.GlobalConfig.UUID,
		runid:             configSpec.GlobalConfig.RUNID,
		waitLimiter:       rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
		functionTemplates: configSpec.GlobalConfig.FunctionTemplates,
		embedCfg:          embedCfg,
		deletionStrategy:  configSpec.GlobalConfig.DeletionStrategy,
		objectOperations:  0,
		backgroundHooks:   make([]*hookProcess, 0),
		hookManager:       NewHookManager(context.Background(), configSpec),
	}

	clientSet, runtimeRestConfig := kubeClientProvider.ClientSet(job.QPS, job.Burst)
	ex.clientSet = clientSet
	ex.restConfig = runtimeRestConfig
	ex.dynamicClient = dynamic.NewForConfigOrDie(ex.restConfig)

	_, setupRestConfig := kubeClientProvider.ClientSet(100, 100) // Hardcoded QPS/Burst
	ex.mapper = newRESTMapper(setupRestConfig)

	switch job.JobType {
	case config.CreationJob:
		ex.setupCreateJob()
	case config.DeletionJob:
		ex.setupDeleteJob()
	case config.PatchJob:
		ex.setupPatchJob()
	case config.ReadJob:
		ex.setupReadJob()
	case config.KubeVirtJob:
		ex.setupKubeVirtJob()
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

func (ex *JobExecutor) renderTemplateForObjectMultiple(obj *object, iteration, replicaIndex int) ([]*unstructured.Unstructured, []*schema.GroupVersionKind) {
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

	objects, gvks := yamlToUnstructuredMultiple(obj.ObjectTemplate, renderedObj)
	return objects, gvks
}
