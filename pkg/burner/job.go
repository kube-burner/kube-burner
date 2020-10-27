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
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type object struct {
	gvr           schema.GroupVersionResource
	objectSpec    []byte
	replicas      int
	unstructured  *unstructured.Unstructured
	inputVars     map[string]string
	labelSelector map[string]string
}

// Executor contains the information required to execute a job
type Executor struct {
	objects  []object
	Start    time.Time
	End      time.Time
	Config   config.Job
	selector *util.Selector
	uuid     string
	limiter  *rate.Limiter
}

// ClientSet kubernetes clientset
var ClientSet *kubernetes.Clientset

var dynamicClient dynamic.Interface

// RestConfig clieng-go rest configuration
var RestConfig *rest.Config

// NewExecutorList Returns a list of executors
func NewExecutorList(uuid string) []Executor {
	var err error
	var executorList []Executor
	RestConfig, err = config.GetRestConfig(0, 0)
	if err != nil {
		log.Fatalf("Error creating restConfig for kube-burner: %s", err)
	}
	ClientSet = kubernetes.NewForConfigOrDie(RestConfig)
	for _, job := range config.ConfigSpec.Jobs {
		ex := getExecutor(job)
		ex.uuid = uuid
		executorList = append(executorList, ex)
	}
	return executorList
}

func getExecutor(jobConfig config.Job) Executor {
	var ex Executor
	if jobConfig.JobType == config.CreationJob {
		ex = setupCreateJob(jobConfig)
	} else if jobConfig.JobType == config.DeletionJob {
		ex = setupDeleteJob(jobConfig)
	} else {
		log.Fatalf("Unknown jobType: %s", jobConfig.JobType)
	}
	// Limits the number of workers to QPS and Burst
	ex.limiter = rate.NewLimiter(rate.Limit(jobConfig.QPS), jobConfig.Burst)
	ex.Config = jobConfig
	return ex
}
