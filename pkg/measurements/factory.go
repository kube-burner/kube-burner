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
	"sync"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type MeasurementsFactory struct {
	metadata  map[string]interface{}
	factories map[string]measurementFactory
}

type Measurements struct {
	measurementsMap map[string]measurement
}

type measurementFactory interface {
	newMeasurement(*config.Job, kubernetes.Interface, *rest.Config) measurement
}
type newMeasurementFactory func(config.Spec, types.Measurement, map[string]interface{}) (measurementFactory, error)

type measurement interface {
	start(*sync.WaitGroup) error
	stop() error
	collect(*sync.WaitGroup)
	index(string, map[string]indexers.Indexer)
	getMetrics() *sync.Map
}

var measurementFactoryMap = map[string]newMeasurementFactory{
	"podLatency":        newPodLatencyMeasurementFactory,
	"pvcLatency":        newPvcLatencyMeasurementFactory,
	"nodeLatency":       newNodeLatencyMeasurementFactory,
	"vmiLatency":        newVmiLatencyMeasurementFactory,
	"serviceLatency":    newServiceLatencyMeasurementFactory,
	"pprof":             newPprofLatencyMeasurementFactory,
	"netpolLatency":     newNetpolLatencyMeasurementFactory,
	"dataVolumeLatency": newDvLatencyMeasurementFactory,
}

func isIndexerOk(configSpec config.Spec, measurement types.Measurement) bool {
	if measurement.QuantilesIndexer != "" || measurement.TimeseriesIndexer != "" {
		for _, indexer := range configSpec.MetricsEndpoints {
			if indexer.Alias == measurement.QuantilesIndexer || indexer.Alias == measurement.TimeseriesIndexer {
				return true
			}
		}
		return false
	}
	return true
}

// NewMeasurementsFactory initializes the measurement facture
func NewMeasurementsFactory(configSpec config.Spec, metadata map[string]interface{}) *MeasurementsFactory {
	measurementsFactory := MeasurementsFactory{
		metadata:  metadata,
		factories: make(map[string]measurementFactory, len(configSpec.GlobalConfig.Measurements)),
	}
	for _, measurement := range configSpec.GlobalConfig.Measurements {
		if !isIndexerOk(configSpec, measurement) {
			log.Fatalf("One of the indexers for measurement %s has not been found", measurement.Name)
		}
		if _, alreadyRegistered := measurementsFactory.factories[measurement.Name]; alreadyRegistered {
			log.Warnf("Measurement [%s] is registered more than once", measurement.Name)
			continue
		}
		newMeasurementFactoryFunc, exists := measurementFactoryMap[measurement.Name]
		if !exists {
			log.Warnf("Measurement [%s] is not supported", measurement.Name)
			continue
		}
		mf, err := newMeasurementFactoryFunc(configSpec, measurement, metadata)
		if err != nil {
			log.Fatal(err.Error())
		}
		measurementsFactory.factories[measurement.Name] = mf
		log.Infof("📈 Registered measurement: %s", measurement.Name)
	}
	return &measurementsFactory
}

func (msf *MeasurementsFactory) NewMeasurements(jobConfig *config.Job, kubeClientProvider *config.KubeClientProvider) *Measurements {
	ms := Measurements{
		measurementsMap: make(map[string]measurement, len(msf.factories)),
	}
	clientSet, restConfig := kubeClientProvider.ClientSet(jobConfig.QPS, jobConfig.Burst)
	for name, factory := range msf.factories {
		ms.measurementsMap[name] = factory.newMeasurement(jobConfig, clientSet, restConfig)
	}

	return &ms
}

// Start starts registered measurements
func (ms *Measurements) Start() {
	var wg sync.WaitGroup
	for _, measurement := range ms.measurementsMap {
		wg.Add(1)
		go measurement.start(&wg)
	}
	wg.Wait()
}

func (ms *Measurements) Collect() {
	var wg sync.WaitGroup
	for _, measurement := range ms.measurementsMap {
		wg.Add(1)
		go measurement.collect(&wg)
	}
	wg.Wait()
}

// Stop stops registered measurements
// returns a concatenated list of error strings with a new line between each string
func (ms *Measurements) Stop() error {
	errs := []error{}
	for name, measurement := range ms.measurementsMap {
		log.Infof("Stopping measurement: %s", name)
		errs = append(errs, measurement.stop())
	}
	return utilerrors.NewAggregate(errs)
}

// Index iterates over the createFuncs map, indexes collected data from each measurement.
//
// jobName is the name of the job to index data for.
// indexerList is a variadic parameter of indexers.Indexer implementations.
func (ms *Measurements) Index(jobName string, indexerList map[string]indexers.Indexer) {
	for name, measurement := range ms.measurementsMap {
		log.Infof("Indexing collected data from measurement: %s", name)
		measurement.index(jobName, indexerList)
	}
}

func (ms *Measurements) GetMetrics() []*sync.Map {
	var metricList []*sync.Map
	for name, measurement := range ms.measurementsMap {
		log.Infof("Fetching metrics from measurement: %s", name)
		metricList = append(metricList, measurement.getMetrics())
	}
	return metricList
}
