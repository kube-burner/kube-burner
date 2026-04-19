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

package burner

import (
	"testing"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
)

func makeExecutor(start, total int, pattern config.LoadPattern) JobExecutor {
	return JobExecutor{
		Job: config.Job{
			JobIterations: start,
			IncrementalLoad: &config.IncrementalLoad{
				StartIterations: start,
				TotalIterations: total,
				Pattern:         pattern,
			},
		},
	}
}

func TestLinearNilConfigNoPanic(t *testing.T) {
	ex := makeExecutor(10, 100, config.LoadPattern{
		Type:   config.LinearPattern,
		Linear: nil,
	})

	calc := NewIterationCalculator(ex)
	lc, ok := calc.(*linearCalculator)
	if !ok {
		t.Fatalf("expected *linearCalculator, got %T", calc)
	}
	if lc.step != 1 {
		t.Errorf("expected step=1 when Linear config is nil, got %d", lc.step)
	}
}

func TestLinearStepSize(t *testing.T) {
	ex := makeExecutor(10, 100, config.LoadPattern{
		Type: config.LinearPattern,
		Linear: &config.LinearLoadConfig{
			StepSize: 15,
		},
	})

	calc := NewIterationCalculator(ex)
	lc, ok := calc.(*linearCalculator)
	if !ok {
		t.Fatalf("expected *linearCalculator, got %T", calc)
	}
	if lc.step != 15 {
		t.Errorf("expected step=15, got %d", lc.step)
	}
}

func TestLinearStepSizeZeroDefaultsToOne(t *testing.T) {
	ex := makeExecutor(10, 100, config.LoadPattern{
		Type: config.LinearPattern,
		Linear: &config.LinearLoadConfig{
			StepSize: 0,
		},
	})

	calc := NewIterationCalculator(ex)
	lc, ok := calc.(*linearCalculator)
	if !ok {
		t.Fatalf("expected *linearCalculator, got %T", calc)
	}
	if lc.step != 1 {
		t.Errorf("expected step=1 for StepSize=0, got %d", lc.step)
	}
}

func TestLinearMinStepsRecalc(t *testing.T) {
	// start=10, total=110, stepSize=100 → totalSteps=ceil(100/100)=1
	// MinSteps=5 → 1 < 5 triggers recalc: step=ceil(100/5)=20
	ex := makeExecutor(10, 110, config.LoadPattern{
		Type: config.LinearPattern,
		Linear: &config.LinearLoadConfig{
			StepSize: 100,
			MinSteps: 5,
		},
	})

	calc := NewIterationCalculator(ex)
	lc, ok := calc.(*linearCalculator)
	if !ok {
		t.Fatalf("expected *linearCalculator, got %T", calc)
	}
	if lc.step != 20 {
		t.Errorf("expected step=20 after MinSteps recalc, got %d", lc.step)
	}
}

func TestExponentialNilConfigNoPanic(t *testing.T) {
	ex := makeExecutor(10, 100, config.LoadPattern{
		Type:        config.ExponentialPattern,
		Exponential: nil,
	})

	calc := NewIterationCalculator(ex)
	ec, ok := calc.(*exponentialCalculator)
	if !ok {
		t.Fatalf("expected *exponentialCalculator, got %T", calc)
	}
	if ec.base != 2.0 {
		t.Errorf("expected default base=2.0, got %f", ec.base)
	}
	if ec.maxIncrease != 0 || ec.warmup != 0 {
		t.Errorf("expected maxIncrease=0 and warmup=0, got maxIncrease=%d warmup=%d", ec.maxIncrease, ec.warmup)
	}
}

func TestExponentialWithConfig(t *testing.T) {
	ex := makeExecutor(10, 100, config.LoadPattern{
		Type: config.ExponentialPattern,
		Exponential: &config.ExponentialLoadConfig{
			Base:        3.0,
			MaxIncrease: 50,
			WarmupSteps: 2,
		},
	})

	calc := NewIterationCalculator(ex)
	ec, ok := calc.(*exponentialCalculator)
	if !ok {
		t.Fatalf("expected *exponentialCalculator, got %T", calc)
	}
	if ec.base != 3.0 {
		t.Errorf("expected base=3.0, got %f", ec.base)
	}
	if ec.maxIncrease != 50 {
		t.Errorf("expected maxIncrease=50, got %d", ec.maxIncrease)
	}
	if ec.warmup != 2 {
		t.Errorf("expected warmup=2, got %d", ec.warmup)
	}
}

func TestZeroTotalIterationsDone(t *testing.T) {
	ex := makeExecutor(0, 0, config.LoadPattern{
		Type: config.LinearPattern,
	})

	calc := NewIterationCalculator(ex)
	lc, ok := calc.(*linearCalculator)
	if !ok {
		t.Fatalf("expected *linearCalculator, got %T", calc)
	}
	_, _, done := lc.Next(0)
	if !done {
		t.Error("expected calculator with total=0 to be done immediately")
	}
}
