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

type MeasurementFactory struct {
	jobConfig   *config.Job
	clientSet   *kubernetes.Clientset
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

var Factory MeasurementFactory
var measurementMap = make(map[string]measurement)
var globalCfg config.GlobalConfig

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(configSpec config.Spec, metadata map[string]interface{}) *MeasurementFactory {
	globalCfg = configSpec.GlobalConfig
	Factory = MeasurementFactory{
		createFuncs: make(map[string]measurement),
		metadata:    metadata,
	}
	for _, measurement := range globalCfg.Measurements {
		if measurementFunc, exists := measurementMap[measurement.Name]; exists {
			if err := Factory.register(measurement, measurementFunc); err != nil {
				log.Fatal(err.Error())
			}
		} else {
			log.Warnf("Measurement not found: %s", measurement.Name)
		}
	}
	return &Factory
}

func (mf *MeasurementFactory) register(measurement types.Measurement, measurementFunc measurement) error {
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

func (mf *MeasurementFactory) SetJobConfig(jobConfig *config.Job) {
	Factory.jobConfig = jobConfig
	_, restConfig, err := config.GetClientSet(Factory.jobConfig.QPS, Factory.jobConfig.Burst)
	if err != nil {
		log.Fatalf("Error creating clientSet: %s", err)
	}
	Factory.clientSet = kubernetes.NewForConfigOrDie(restConfig)
	Factory.restConfig = restConfig
}

// Start starts registered measurements
func (mf *MeasurementFactory) Start() {
	var wg sync.WaitGroup
	for _, measurement := range Factory.createFuncs {
		wg.Add(1)
		go measurement.start(&wg)
	}
	wg.Wait()
}

func (mf *MeasurementFactory) Collect() {
	var wg sync.WaitGroup
	for _, measurement := range Factory.createFuncs {
		wg.Add(1)
		go measurement.collect(&wg)
	}
	wg.Wait()
}

// Stop stops registered measurements
// returns a concatenated list of error strings with a new line between each string
func (mf *MeasurementFactory) Stop() error {
	errs := []error{}
	for name, measurement := range Factory.createFuncs {
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
func (mf *MeasurementFactory) Index(indexer *indexers.Indexer, jobName string) {
	for name, measurement := range Factory.createFuncs {
		log.Infof("Indexing collected data from measurement: %s", name)
		measurement.index(*indexer, jobName)
	}
}
