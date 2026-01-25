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
	"context"
	"math"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
)

// IterationCalculator returns the next iteration window for incremental runs.
type IterationCalculator interface {
	Next(current int) (start, end int, done bool)
}

// NewIterationCalculator returns an IterationCalculator based on job's IncrementalLoad config.
func NewIterationCalculator(ex JobExecutor) IterationCalculator {
	cfg := ex.IncrementalLoad
	minIt := cfg.MinIterations
	if minIt <= 0 {
		minIt = ex.Job.JobIterations
	}
	// Determine final maximum iterations: prefer IncrementalLoad.MaxIterations, fall back to JobIterations
	maxIt := cfg.MaxIterations
	if maxIt <= 0 {
		maxIt = minIt
	}
	if maxIt <= 0 {
		// nothing to do
		return &linearCalculator{min: 0, max: 0, step: 0}
	}
	switch cfg.Pattern.Type {
	case config.LinearPattern:
		step := 0
		if cfg.Pattern.Linear != nil {
			step = cfg.Pattern.Linear.StepSize
		}
		// derive step from MinSteps if provided
		if cfg.MinSteps > 0 && step < cfg.MinSteps {
			remaining := maxIt - minIt
			if remaining <= 0 {
				step = maxIt
			} else {
				step = int(math.Ceil(float64(remaining) / float64(cfg.MinSteps)))
			}
		}
		if step <= 0 {
			step = 1
		}
		return &linearCalculator{min: minIt, max: maxIt, step: step}
	case config.ExponentialPattern:
		base := 2.0
		maxInc := cfg.Pattern.Exponential.MaxIncrease
		warmup := cfg.Pattern.Exponential.WarmupSteps
		if cfg.Pattern.Exponential != nil && cfg.Pattern.Exponential.Base > 0 {
			base = cfg.Pattern.Exponential.Base
		}
		return &exponentialCalculator{min: minIt, max: maxIt, base: base, maxIncrease: maxInc, warmup: warmup, stepNo: 0}
	default:
		// default to linear behaviour
		step := 0
		if cfg.MinSteps > 0 {
			remaining := maxIt - minIt
			step = int(math.Ceil(float64(remaining) / float64(cfg.MinSteps)))
		} 
		if step <= 0 {
			step = 1
		}
		return &linearCalculator{min: minIt, max: maxIt, step: step}
	}
}

// linearCalculator increments iterations by a fixed step until max is reached.
type linearCalculator struct {
	min  int
	max  int
	step int
}

func (l *linearCalculator) Next(current int) (start, end int, done bool) {
	if l.max <= 0 {
		return 0, 0, true
	}
	// first step: create min iterations
	if current == 0 {
		next := l.min
		if next > l.max {
			next = l.max
		}
		return 0, next, false
	}
	next := current + l.step
	if next > l.max {
		next = l.max
	}
	return current, next, false
}

// exponentialCalculator increases iterations exponentially after an optional warmup.
type exponentialCalculator struct {
	min         int
	max         int
	base        float64
	maxIncrease int
	warmup      int
	stepNo      int
}

func (e *exponentialCalculator) Next(current int) (start, end int, done bool) {
	if e.max <= 0 {
		return 0, 0, true
	}
	// first step
	if current == 0 {
		e.stepNo = 1
		next := e.min
		if next > e.max {
			next = e.max
		}
		return 0, next, false
	}
	e.stepNo++
	// warmup: linear increments of min
	if e.warmup > 0 && e.stepNo <= e.warmup {
		candidate := current + e.min
		if candidate > e.max {
			candidate = e.max
		}
		return current, candidate, false
	}
	// exponential increase based on base^(stepNo-warmup)
	expPower := float64(e.stepNo)
	if e.warmup > 0 {
		expPower = float64(e.stepNo - e.warmup)
	}
	increase := int(math.Pow(e.base, expPower)) * e.min
	if increase <= 0 {
		increase = 1
	}
	if e.maxIncrease > 0 && increase > e.maxIncrease {
		increase = e.maxIncrease
	}
	candidate := current + increase
	if candidate > e.max {
		candidate = e.max
	}
	return current, candidate, false
}

// RunIncrementalCreateJob executes incremental steps using the provided calculator.
func RunIncrementalCreateJob(
	ctx context.Context,
	ex JobExecutor,
	calculator IterationCalculator,
	stepDelay time.Duration,
) []error {

	current := 0
	var allErrs []error

	for {
		start, end, done := calculator.Next(current)
		if done {
			log.Infof("Incremental creation completed")
			return allErrs
		}

		log.Infof("Running incremental step: iterations [%d → %d)", start, end)

		if errs := ex.RunCreateJob(ctx, start, end); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
			return allErrs
		}

		// run executor health check if provided
		if ex.HealthCheckFunc != nil {
			if err := ex.HealthCheckFunc(ctx); err != nil {
				allErrs = append(allErrs, err)
				return allErrs
			}
		}

		if stepDelay > 0 {
			log.Infof("Sleeping %v before next step", stepDelay)
			time.Sleep(stepDelay)
		}

		current = end
	}
}
