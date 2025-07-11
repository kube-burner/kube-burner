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

	"dario.cat/mergo"
	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type MeasurementsFactory struct {
	Metadata   map[string]any
	Factories  map[string]MeasurementFactory
	ConfigSpec config.Spec
}

type Measurements struct {
	MeasurementsMap map[string]Measurement
}

type MeasurementFactory interface {
	NewMeasurement(*config.Job, kubernetes.Interface, *rest.Config, *fileutils.EmbedConfiguration) Measurement
}
type NewMeasurementFactory func(config.Spec, types.Measurement, map[string]any) (MeasurementFactory, error)

type Measurement interface {
	Start(*sync.WaitGroup) error
	Stop() error
	Collect(*sync.WaitGroup)
	Index(string, map[string]indexers.Indexer)
	GetMetrics() *sync.Map
}

var measurementFactoryMap = map[string]NewMeasurementFactory{
	"podLatency":            newPodLatencyMeasurementFactory,
	"jobLatency":            newJobLatencyMeasurementFactory,
	"pvcLatency":            newPvcLatencyMeasurementFactory,
	"nodeLatency":           newNodeLatencyMeasurementFactory,
	"vmiLatency":            newVmiLatencyMeasurementFactory,
	"serviceLatency":        newServiceLatencyMeasurementFactory,
	"pprof":                 newPprofLatencyMeasurementFactory,
	"netpolLatency":         newNetpolLatencyMeasurementFactory,
	"dataVolumeLatency":     newDvLatencyMeasurementFactory,
	"volumeSnapshotLatency": newvolumeSnapshotLatencyMeasurementFactory,
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
func NewMeasurementsFactory(configSpec config.Spec, metadata map[string]any, additionalMeasurementFactoryMap map[string]NewMeasurementFactory) *MeasurementsFactory {
	// Add from additionalMeasurementFactoryMap without overwriting
	for k, v := range additionalMeasurementFactoryMap {
		if _, exists := measurementFactoryMap[k]; !exists {
			measurementFactoryMap[k] = v
		}
	}

	measurementsFactory := MeasurementsFactory{
		Metadata:   metadata,
		Factories:  make(map[string]MeasurementFactory, len(configSpec.GlobalConfig.Measurements)),
		ConfigSpec: configSpec,
	}
	for _, measurement := range configSpec.GlobalConfig.Measurements {
		if !isIndexerOk(configSpec, measurement) {
			log.Fatalf("One of the indexers for measurement %s has not been found", measurement.Name)
		}
		if _, alreadyRegistered := measurementsFactory.Factories[measurement.Name]; alreadyRegistered {
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
		measurementsFactory.Factories[measurement.Name] = mf
		log.Infof("📈 Registered measurement: %s", measurement.Name)
	}
	return &measurementsFactory
}

func (msf *MeasurementsFactory) NewMeasurements(jobConfig *config.Job, kubeClientProvider *config.KubeClientProvider, embedCfg *fileutils.EmbedConfiguration) *Measurements {
	ms := Measurements{
		MeasurementsMap: make(map[string]Measurement),
	}
	clientSet, restConfig := kubeClientProvider.ClientSet(jobConfig.QPS, jobConfig.Burst)

	log.Infof("Initializing measurements for job: %s", jobConfig.Name)
	mergedMeasurements := make(map[string]types.Measurement)
	for _, measurement := range msf.ConfigSpec.GlobalConfig.Measurements {
		mergedMeasurements[measurement.Name] = measurement
	}
	for _, measurement := range jobConfig.Measurements {
		if globalMeasurement, exists := mergedMeasurements[measurement.Name]; exists {
			if err := mergo.Merge(&globalMeasurement, measurement, mergo.WithOverride); err != nil {
				log.Errorf("Failed to merge measurement [%s]: %v", measurement.Name, err)
				continue
			}
			mergedMeasurements[measurement.Name] = globalMeasurement
		} else {
			mergedMeasurements[measurement.Name] = measurement
		}
	}
	for name, measurement := range mergedMeasurements {
		newMeasurementFactoryFunc, exists := measurementFactoryMap[name]
		if !exists {
			log.Warnf("Measurement [%s] is not supported", name)
			continue
		}
		mf, err := newMeasurementFactoryFunc(msf.ConfigSpec, measurement, msf.Metadata)
		if err != nil {
			log.Errorf("Failed to create measurement [%s]: %v", name, err)
			continue
		}
		ms.MeasurementsMap[name] = mf.NewMeasurement(jobConfig, clientSet, restConfig, embedCfg)
		log.Infof("Registered measurement: %s", name)
	}

	return &ms
}

// Start starts registered measurements
func (ms *Measurements) Start() {
	var wg sync.WaitGroup
	for _, measurement := range ms.MeasurementsMap {
		wg.Add(1)
		go measurement.Start(&wg)
	}
	wg.Wait()
}

func (ms *Measurements) Collect() {
	var wg sync.WaitGroup
	for _, measurement := range ms.MeasurementsMap {
		wg.Add(1)
		go measurement.Collect(&wg)
	}
	wg.Wait()
}

// Stop stops registered measurements
// returns a concatenated list of error strings with a new line between each string
func (ms *Measurements) Stop() error {
	errs := []error{}
	for name, measurement := range ms.MeasurementsMap {
		log.Infof("Stopping measurement: %s", name)
		errs = append(errs, measurement.Stop())
	}
	return utilerrors.NewAggregate(errs)
}

// Index iterates over the createFuncs map, indexes collected data from each measurement.
//
// jobName is the name of the job to index data for.
// indexerList is a variadic parameter of indexers.Indexer implementations.
func (ms *Measurements) Index(jobName string, indexerList map[string]indexers.Indexer) {
	for name, measurement := range ms.MeasurementsMap {
		log.Infof("Indexing collected data from measurement: %s", name)
		measurement.Index(jobName, indexerList)
	}
}

func (ms *Measurements) GetMetrics() []*sync.Map {
	var metricList []*sync.Map
	for name, measurement := range ms.MeasurementsMap {
		log.Infof("Fetching metrics from measurement: %s", name)
		metricList = append(metricList, measurement.GetMetrics())
	}
	return metricList
}
