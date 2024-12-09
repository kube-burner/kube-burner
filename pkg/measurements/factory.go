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
	"embed"
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
	createFuncs map[string]Measurement
	metadata    map[string]interface{}
}

type Measurement interface {
	start(*sync.WaitGroup) error
	stop() error
	collect(*sync.WaitGroup)
	setConfig(types.MeasurementConfig) error
	index(string, map[string]indexers.Indexer)
	getMetrics() *sync.Map
}

var factory measurementFactory
var measurementMap = make(map[string]Measurement)
var globalCfg config.GlobalConfig
var embedFS embed.FS
var embedFSDir string

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(configSpec config.Spec, metadata map[string]interface{}) {
	var indexerFound bool
	embedFS = configSpec.EmbedFS
	embedFSDir = configSpec.EmbedFSDir
	globalCfg = configSpec.GlobalConfig
	factory = measurementFactory{
		createFuncs: make(map[string]Measurement),
		metadata:    metadata,
	}
	for _, measurement := range globalCfg.Measurements {
		indexerFound = false
		if measurement.Config.QuantilesIndexer != "" || measurement.Config.TimeseriesIndexer != "" {
			for _, indexer := range configSpec.MetricsEndpoints {
				if indexer.Alias == measurement.Config.QuantilesIndexer || indexer.Alias == measurement.Config.TimeseriesIndexer {
					indexerFound = true
					break
				}
			}
			if !indexerFound {
				log.Fatalf("One of the indexers for measurement %s has not been found", measurement.Name)
			}
		}
		if measurementFunc, exists := measurementMap[measurement.Name]; exists {
			if err := factory.Register(measurement, measurementFunc); err != nil {
				log.Fatal(err.Error())
			}
		} else {
			log.Warnf("Measurement not found: %s", measurement.Name)
		}
	}
}

// Registers a new measurement
func (mf *measurementFactory) Register(measurement types.Measurement, measurementFunc Measurement) error {
	if _, exists := mf.createFuncs[measurement.Name]; exists {
		log.Warnf("Measurement already registered: %s", measurement.Name)
	} else {
		if err := measurementFunc.setConfig(measurement.Config); err != nil {
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

// Index iterates over the createFuncs map, indexes collected data from each measurement.
//
// jobName is the name of the job to index data for.
// indexerList is a variadic parameter of indexers.Indexer implementations.
func Index(jobName string, indexerList map[string]indexers.Indexer) {
	for name, measurement := range factory.createFuncs {
		log.Infof("Indexing collected data from measurement: %s", name)
		measurement.index(jobName, indexerList)
	}
}

func GetMetrics() []*sync.Map {
	var metricList []*sync.Map
	for name, measurement := range factory.createFuncs {
		log.Infof("Fetching metrics from measurement: %s", name)
		metricList = append(metricList, measurement.getMetrics())
	}
	return metricList
}

func Load(name string, measurement Measurement) {
	log.Debugf("Loading external measurement into measurementMap: %s", name)
	measurementMap[name] = measurement
}
