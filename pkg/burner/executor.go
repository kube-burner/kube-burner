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
	"sync"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
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
type ItemHandler func(ex *Executor, obj object, originalItem unstructured.Unstructured, iteration int, wg *sync.WaitGroup)
type ObjectFinalizer func(ex *Executor, obj object)

type Executor struct {
	config.Job
	objects         []object
	uuid            string
	runid           string
	limiter         *rate.Limiter
	waitLimiter     *rate.Limiter
	nsRequired      bool
	itemHandler     ItemHandler
	objectFinalizer ObjectFinalizer
	clientSet       kubernetes.Interface
	restConfig      *rest.Config
	dynamicClient   *dynamic.DynamicClient
	kubeVirtClient  kubecli.KubevirtClient
}

func newExecutor(configSpec config.Spec, kubeClientProvider *config.KubeClientProvider, job config.Job) Executor {
	ex := Executor{
		Job:         job,
		limiter:     rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
		uuid:        configSpec.GlobalConfig.UUID,
		runid:       configSpec.GlobalConfig.RUNID,
		waitLimiter: rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
	}

	clientSet, runtimeRestConfig := kubeClientProvider.ClientSet(job.QPS, job.Burst)
	ex.clientSet = clientSet
	ex.restConfig = runtimeRestConfig
	ex.dynamicClient = dynamic.NewForConfigOrDie(ex.restConfig)

	_, setupRestConfig := kubeClientProvider.ClientSet(100, 100) // Hardcoded QPS/Burst
	mapper := newRESTMapper(discovery.NewDiscoveryClientForConfigOrDie(setupRestConfig))

	switch job.JobType {
	case config.CreationJob:
		ex.setupCreateJob(configSpec, mapper)
	case config.DeletionJob:
		ex.setupDeleteJob(configSpec, mapper)
	case config.PatchJob:
		ex.setupPatchJob(configSpec, mapper)
	case config.ReadJob:
		ex.setupReadJob(configSpec, mapper)
	case config.KubeVirtJob:
		ex.setupKubeVirtJob(configSpec, mapper)
	default:
		log.Fatalf("Unknown jobType: %s", job.JobType)
	}
	return ex
}

func (ex *Executor) renderTemplateForObject(obj object, iteration, replicaIndex int, asJson bool) []byte {
	// Processing template
	templateData := map[string]interface{}{
		jobName:      ex.Name,
		jobIteration: iteration,
		jobUUID:      ex.uuid,
		replica:      replicaIndex,
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
