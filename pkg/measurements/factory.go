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

package measurements

import (
	"fmt"
	"sync"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type measurementFactory struct {
	jobConfig   *config.Job
	clientSet   kubernetes.Interface
	restConfig  *rest.Config
	createFuncs map[string]measurement
	metadata    map[string]interface{}
}

type measurement interface {
	start(*sync.WaitGroup) error
	stop() error
	collect(*sync.WaitGroup)
	setConfig(types.Measurement)
	validateConfig() error
	index(indexers.Indexer, string)
}

var factory measurementFactory
var measurementMap = make(map[string]measurement)
var globalCfg config.GlobalConfig

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(configSpec config.Spec, metadata map[string]interface{}) {
	globalCfg = configSpec.GlobalConfig
	factory = measurementFactory{
		createFuncs: make(map[string]measurement),
		metadata:    metadata,
	}
	for _, measurement := range globalCfg.Measurements {
		if measurementFunc, exists := measurementMap[measurement.Name]; exists {
			if err := factory.register(measurement, measurementFunc); err != nil {
				log.Fatal(err.Error())
			}
		} else {
			log.Warnf("Measurement not found: %s", measurement.Name)
		}
	}
}

func (mf *measurementFactory) register(measurement types.Measurement, measurementFunc measurement) error {
	if _, exists := mf.createFuncs[measurement.Name]; exists {
		log.Warnf("Measurement already registered: %s", measurement.Name)
	} else {
		measurementFunc.setConfig(measurement)
		if err := measurementFunc.validateConfig(); err != nil {
			return fmt.Errorf("%s config error: %s", measurement.Name, err)
		}
		mf.createFuncs[measurement.Name] = measurementFunc
		log.Infof("📈 Registered measurement: %s", measurement.Name)
	}
	return nil
}

func SetJobConfig(jobConfig *config.Job, kubeClientProvider *config.KubeClientProvider) {
	factory.jobConfig = jobConfig
	factory.clientSet, factory.restConfig = kubeClientProvider.ClientSet(factory.jobConfig.QPS, factory.jobConfig.Burst)
}

// Start starts registered measurements
func Start() {
	var wg sync.WaitGroup
	for _, measurement := range factory.createFuncs {
		wg.Add(1)
		go measurement.start(&wg)
	}
	wg.Wait()
}

func Collect() {
	var wg sync.WaitGroup
	for _, measurement := range factory.createFuncs {
		wg.Add(1)
		go measurement.collect(&wg)
	}
	wg.Wait()
}

// Stop stops registered measurements
// returns a concatenated list of error strings with a new line between each string
func Stop() error {
	errs := []error{}
	for name, measurement := range factory.createFuncs {
		log.Infof("Stopping measurement: %s", name)
		errs = append(errs, measurement.stop())
	}
	return utilerrors.NewAggregate(errs)
}

// Index iterates through the createFuncs and indexes collected data from each measurement.
//
// Parameters:
//   - indexer: a pointer to the indexer
//   - jobName: the name of the job, required as this task can be executed from a goroutine
func Index(indexer indexers.Indexer, jobName string) {
	for name, measurement := range factory.createFuncs {
		log.Infof("Indexing collected data from measurement: %s", name)
		measurement.index(indexer, jobName)
	}
}
