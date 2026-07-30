// Copyright 2026 The Kube-burner Authors.
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
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	log "github.com/sirupsen/logrus"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

type fakeMeasurement struct {
	startErr     error
	startCalls   atomic.Int32
	stopCalls    atomic.Int32
	collectCalls atomic.Int32
	metrics      sync.Map
}

func (f *fakeMeasurement) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	f.startCalls.Add(1)
	return f.startErr
}

func (f *fakeMeasurement) Stop() error {
	f.stopCalls.Add(1)
	return nil
}

func (f *fakeMeasurement) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	f.collectCalls.Add(1)
}

func (f *fakeMeasurement) IsCompatible() bool {
	return true
}

func (f *fakeMeasurement) Index(string, map[string]indexers.Indexer) {
}

func (f *fakeMeasurement) GetMetrics() *sync.Map {
	return &f.metrics
}

func TestMeasurementsStopSkipsFailedStarts(t *testing.T) {
	var logOutput bytes.Buffer
	logger := log.StandardLogger()
	previousOutput := logger.Out
	logger.SetOutput(&logOutput)
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
	})

	failed := &fakeMeasurement{startErr: errors.New("discovery unavailable")}
	started := &fakeMeasurement{}
	ms := Measurements{
		MeasurementsMap: map[string]Measurement{
			"failed":  failed,
			"started": started,
		},
	}

	ms.Start()
	if err := ms.Stop(); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}

	if failed.startCalls.Load() != 1 {
		t.Fatalf("failed measurement Start() called %d times, want 1", failed.startCalls.Load())
	}
	if failed.stopCalls.Load() != 0 {
		t.Fatalf("failed measurement Stop() called %d times, want 0", failed.stopCalls.Load())
	}
	if started.stopCalls.Load() != 1 {
		t.Fatalf("started measurement Stop() called %d times, want 1", started.stopCalls.Load())
	}
	if !strings.Contains(logOutput.String(), "Failed to start measurement [failed]: discovery unavailable") {
		t.Fatalf("startup failure was not logged: %s", logOutput.String())
	}
	if !strings.Contains(logOutput.String(), "Skipping measurement [failed] because it failed to start: discovery unavailable") {
		t.Fatalf("failed measurement skip was not logged: %s", logOutput.String())
	}
}

func TestMeasurementsStopWithoutStart(t *testing.T) {
	measurement := &fakeMeasurement{}
	ms := Measurements{
		MeasurementsMap: map[string]Measurement{
			"collect-only": measurement,
		},
	}

	ms.Collect()
	if err := ms.Stop(); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}

	if measurement.collectCalls.Load() != 1 {
		t.Fatalf("measurement Collect() called %d times, want 1", measurement.collectCalls.Load())
	}
	if measurement.stopCalls.Load() != 1 {
		t.Fatalf("measurement Stop() called %d times, want 1", measurement.stopCalls.Load())
	}
}

func TestMeasurementStopHandlesUninitializedChannels(t *testing.T) {
	jobConfig := &config.Job{
		Name:            "test",
		NamespaceLabels: map[string]string{},
	}
	clientSet := k8sfake.NewSimpleClientset()

	originalPortForwarder := proxyPortForwarder
	originalConnections := connections
	proxyPortForwarder = nil
	connections = make(map[string][]connection)
	t.Cleanup(func() {
		proxyPortForwarder = originalPortForwarder
		connections = originalConnections
	})

	tests := map[string]Measurement{
		"podLatency": &podLatency{
			BaseMeasurement: BaseMeasurement{JobConfig: jobConfig},
		},
		"serviceLatency": &serviceLatency{
			BaseMeasurement: BaseMeasurement{
				JobConfig: jobConfig,
				ClientSet: clientSet,
			},
		},
		"pprof": &pprof{
			BaseMeasurement: BaseMeasurement{JobConfig: jobConfig},
		},
		"netpolLatency": &netpolLatency{
			BaseMeasurement: BaseMeasurement{
				JobConfig: jobConfig,
				ClientSet: clientSet,
			},
		},
	}

	for name, measurement := range tests {
		t.Run(name, func(t *testing.T) {
			if err := measurement.Stop(); err != nil {
				t.Fatalf("unexpected stop error: %v", err)
			}
		})
	}
}
