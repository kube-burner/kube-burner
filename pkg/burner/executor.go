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
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Executor contains the information required to execute a job
type ItemHandler func(ex *Executor, obj object, originalItem unstructured.Unstructured, iteration int, wg *sync.WaitGroup)
type ObjectFinalizer func(obj object)

type Executor struct {
	config.Job
	objects         []object
	uuid            string
	runid           string
	limiter         *rate.Limiter
	nsRequired      bool
	itemHandler     ItemHandler
	objectFinalizer ObjectFinalizer
}

func newExecutor(job config.Job, uuid, runid string) *Executor {
	ex := &Executor{
		Job:     job,
		limiter: rate.NewLimiter(rate.Limit(job.QPS), job.Burst),
		uuid:    uuid,
		runid:   runid,
	}

	switch job.JobType {
	case config.CreationJob:
		setupCreateJob(ex)
	case config.DeletionJob:
		setupDeleteJob(ex)
	case config.PatchJob:
		setupPatchJob(ex)
	case config.ReadJob:
		setupReadJob(ex)
	default:
		log.Fatalf("Unknown jobType: %s", job.JobType)
	}

	return ex
}
