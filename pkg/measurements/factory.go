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

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/watcher"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type measurementFactory struct {
	JobConfig   *config.Job
	ClientSet   *kubernetes.Clientset
	RestConfig  *rest.Config
	CreateFuncs map[string]Measurement
	Indexer     *indexers.Indexer
	UUID        string
}

type Measurement interface {
	Start(*sync.WaitGroup)
	Stop() (int, error)
	SetConfig(types.Measurement) error
	RegisterWatcher(name string, w *watcher.Watcher)
	GetWatcher(name string) (*watcher.Watcher, bool)
}

var factory measurementFactory
var MeasurementMap = make(map[string]Measurement)
var kubeburnerCfg *config.GlobalConfig = &config.ConfigSpec.GlobalConfig

// NewMeasurementFactory initializes the measurement facture
func NewMeasurementFactory(restConfig *rest.Config, uuid string, indexer *indexers.Indexer) {
	log.Info("📈 Creating measurement factory")
	clientSet := kubernetes.NewForConfigOrDie(restConfig)
	factory = measurementFactory{
		ClientSet:   clientSet,
		RestConfig:  restConfig,
		CreateFuncs: make(map[string]Measurement),
		Indexer:     indexer,
		UUID:        uuid,
	}
	for _, measurement := range kubeburnerCfg.Measurements {
		CreateMeasurementIfNotExist(&measurement, measurement.Name)
	}
}

// CreateMeasurementIfNotExist creates a new measurement if structure is empty
func CreateMeasurementIfNotExist(measurement *types.Measurement, measurementType string) {
	if measurementFunc, exists := MeasurementMap[measurementType]; exists {
		if measurement == nil || measurement.Name == "" {
			measurement = defaultMeasurement(measurementType)
		}
		if err := factory.register(*measurement, measurementFunc); err != nil {
			log.Fatal(err.Error())
		}
	} else {
		log.Warnf("Measurement not found: %s", measurement.Name)
	}
}

func defaultMeasurement(measurementType string) *types.Measurement {
	return &types.Measurement{
		Name:    measurementType,
		ESIndex: "kube-burner",
	}
}

func (mf *measurementFactory) register(measurement types.Measurement, measurementFunc Measurement) error {
	if _, exists := mf.CreateFuncs[measurement.Name]; exists {
		log.Warnf("Measurement already registered: %s", measurement.Name)
	} else {
		if err := measurementFunc.SetConfig(measurement); err != nil {
			return fmt.Errorf("config validataion error: %s", err)
		}
		mf.CreateFuncs[measurement.Name] = measurementFunc
		log.Infof("Registered measurement: %s", measurement.Name)
	}
	return nil
}

func SetJobConfig(jobConfig *config.Job) {
	factory.JobConfig = jobConfig
}

// Start starts registered measurements
func Start(wg *sync.WaitGroup) {
	for _, measurement := range factory.CreateFuncs {
		wg.Add(1)
		go measurement.Start(wg)
	}
}

// Stop stops registered measurements
func Stop() int {
	var err error
	var r, rc int
	for name, measurement := range factory.CreateFuncs {
		log.Infof("Stopping measurement: %s", name)
		if r, err = measurement.Stop(); err != nil {
			log.Errorf("Error stopping measurement %s: %s", name, err)
		}
		if r != 0 {
			rc = r
		}
	}
	return rc
}
