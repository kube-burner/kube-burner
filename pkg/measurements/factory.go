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
	"github.com/rsevilla87/kube-burner/log"
	"github.com/rsevilla87/kube-burner/pkg/config"
	"github.com/rsevilla87/kube-burner/pkg/indexers"
	"k8s.io/client-go/kubernetes"
)

type measurementFactory struct {
	globalConfig config.GlobalConfig
	config       config.Job
	clientSet    *kubernetes.Clientset
	createFuncs  map[string]measurement
	indexer      *indexers.Indexer
	uuid         string
}

type measurement interface {
	Start(measurementFactory)
	SetMetadata(string)
	Index()
	writeToFile() error
	waitForReady()
	Stop() error
}

var factory measurementFactory
var measurementMap = make(map[string]measurement)

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(clientSet *kubernetes.Clientset, globalConfig config.GlobalConfig, config config.Job, uuid string, indexer *indexers.Indexer) {
	log.Info("📈 Creating measurement factory")
	factory = measurementFactory{
		globalConfig: globalConfig,
		config:       config,
		clientSet:    clientSet,
		createFuncs:  make(map[string]measurement),
		indexer:      indexer,
		uuid:         uuid,
	}
}

func (mf *measurementFactory) register(methodName string, indexName string, measurementFunc measurement) {
	if _, exists := mf.createFuncs[methodName]; exists {
		log.Warnf("Measurement already registered: %s", methodName)
	} else {
		measurementFunc.SetMetadata(indexName)
		mf.createFuncs[methodName] = measurementFunc
		log.Infof("Registered measurement %s", methodName)
	}
}

// Register registers the given list of measurements
func Register(measurementList []config.Measurement) {
	for _, measurement := range measurementList {
		if measurementFunc, exists := measurementMap[measurement.Name]; exists {
			factory.register(measurement.Name, measurement.ESIndex, measurementFunc)
		} else {
			log.Warnf("Measurement not found: %s", measurement.Name)
		}
	}
}

// Start starts registered measurements
func Start() {
	factory.start()
}

func (mf *measurementFactory) start() {
	for _, measurement := range mf.createFuncs {
		go measurement.Start(factory)
		measurement.waitForReady()
	}
}

// Stop stops registered measurements
func Stop() {
	for name, measurement := range factory.createFuncs {
		if err := measurement.Stop(); err != nil {
			log.Errorf("Error stopping measurement %s: %s", name, err)
		}
	}
}

func (mf *measurementFactory) index() {
	for name, measurement := range mf.createFuncs {
		if mf.globalConfig.WriteToFile {
			if err := measurement.writeToFile(); err != nil {
				log.Errorf("Error writing measurement %s: %s", name, err)
			}
		}
		if factory.globalConfig.IndexerConfig.Enabled {
			measurement.Index()
		}
	}
}

// Index index measurements metrics
func Index() {
	factory.index()
}
