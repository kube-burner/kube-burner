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
	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type measurementFactory struct {
	globalConfig config.GlobalConfig
	config       config.Job
	clientSet    *kubernetes.Clientset
	restConfig   *rest.Config
	createFuncs  map[string]measurement
	indexer      *indexers.Indexer
	uuid         string
}

type measurement interface {
	start()
	stop() error
	setConfig(config.Measurement)
}

var factory measurementFactory
var measurementMap = make(map[string]measurement)

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(restConfig *rest.Config, jobConfig config.Job, uuid string, indexer *indexers.Indexer) {
	log.Info("📈 Creating measurement factory")
	clientSet := kubernetes.NewForConfigOrDie(restConfig)
	factory = measurementFactory{
		globalConfig: config.ConfigSpec.GlobalConfig,
		config:       jobConfig,
		clientSet:    clientSet,
		restConfig:   restConfig,
		createFuncs:  make(map[string]measurement),
		indexer:      indexer,
		uuid:         uuid,
	}
}

func (mf *measurementFactory) register(measure config.Measurement, measurementFunc measurement) {
	if _, exists := mf.createFuncs[measure.Name]; exists {
		log.Warnf("Measurement already registered: %s", measure.Name)
	} else {
		measurementFunc.setConfig(measure)
		mf.createFuncs[measure.Name] = measurementFunc
		log.Infof("Registered measurement: %s", measure.Name)
	}
}

// Register registers the given list of measurements
func Register() {
	for _, measurement := range config.ConfigSpec.GlobalConfig.Measurements {
		if measurementFunc, exists := measurementMap[measurement.Name]; exists {
			factory.register(measurement, measurementFunc)
		} else {
			log.Warnf("Measurement not found: %s", measurement.Name)
		}
	}
}

// Start starts registered measurements
func Start() {
	for _, measurement := range factory.createFuncs {
		go measurement.start()
	}
}

// Stop stops registered measurements
func Stop() {
	for name, measurement := range factory.createFuncs {
		log.Infof("Stopping measurement: %s", name)
		if err := measurement.stop(); err != nil {
			log.Errorf("Error stopping measurement %s: %s", name, err)
		}
	}
}
