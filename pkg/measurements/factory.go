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
	clientSet   *kubernetes.Clientset
	restConfig  *rest.Config
	createFuncs map[string]measurement
	indexer     *indexers.Indexer
	metadata    map[string]interface{}
}

type measurement interface {
	start(*sync.WaitGroup)
	stop() error
	collect(*sync.WaitGroup)
	setConfig(types.Measurement) error
}

var factory measurementFactory
var measurementMap = make(map[string]measurement)
var globalCfg config.GlobalConfig

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(configSpec config.Spec, indexer *indexers.Indexer, metadata map[string]interface{}) {
	globalCfg = configSpec.GlobalConfig
	_, restConfig, err := config.GetClientSet(0, 0)
	if err != nil {
		log.Fatalf("Error creating clientSet: %s", err)
	}
	clientSet := kubernetes.NewForConfigOrDie(restConfig)
	factory = measurementFactory{
		clientSet:   clientSet,
		restConfig:  restConfig,
		createFuncs: make(map[string]measurement),
		indexer:     indexer,
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
		if err := measurementFunc.setConfig(measurement); err != nil {
			return fmt.Errorf("Config validation error: %s", err)
		}
		mf.createFuncs[measurement.Name] = measurementFunc
		log.Infof("📈 Registered measurement: %s", measurement.Name)
	}
	return nil
}

func SetJobConfig(jobConfig *config.Job) {
	factory.jobConfig = jobConfig
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
