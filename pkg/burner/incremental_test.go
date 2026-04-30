// Copyright 2024 The Kube-burner Authors.
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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
)

func TestBurner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Burner Suite")
}

func newTestExecutor(il config.IncrementalLoad) JobExecutor {
	return JobExecutor{
		Job: config.Job{
			JobIterations:   10,
			IncrementalLoad: &il,
		},
	}
}

var _ = Describe("NewIterationCalculator", func() {
	Describe("exponential pattern", func() {
		Context("when exponential config block is missing", func() {
			It("should not panic and use safe defaults", func() {
				ex := newTestExecutor(config.IncrementalLoad{
					StartIterations: 10,
					TotalIterations: 100,
					Pattern: config.LoadPattern{
						Type:        config.ExponentialPattern,
						Exponential: nil,
					},
				})
				calc := NewIterationCalculator(ex)
				Expect(calc).NotTo(BeNil())

				ec, ok := calc.(*exponentialCalculator)
				Expect(ok).To(BeTrue(), "expected *exponentialCalculator")
				Expect(ec.base).To(Equal(2.0))
				Expect(ec.maxIncrease).To(Equal(0))
				Expect(ec.warmup).To(Equal(0))
			})
		})

		Context("when exponential config block is provided", func() {
			It("should use values from config", func() {
				ex := newTestExecutor(config.IncrementalLoad{
					StartIterations: 10,
					TotalIterations: 100,
					Pattern: config.LoadPattern{
						Type: config.ExponentialPattern,
						Exponential: &config.ExponentialLoadConfig{
							Base:        3.0,
							MaxIncrease: 50,
							WarmupSteps: 2,
						},
					},
				})
				calc := NewIterationCalculator(ex)
				ec, ok := calc.(*exponentialCalculator)
				Expect(ok).To(BeTrue(), "expected *exponentialCalculator")
				Expect(ec.base).To(Equal(3.0))
				Expect(ec.maxIncrease).To(Equal(50))
				Expect(ec.warmup).To(Equal(2))
			})

			It("should fall back to base=2.0 when Base is zero", func() {
				ex := newTestExecutor(config.IncrementalLoad{
					StartIterations: 10,
					TotalIterations: 100,
					Pattern: config.LoadPattern{
						Type: config.ExponentialPattern,
						Exponential: &config.ExponentialLoadConfig{
							Base:        0,
							MaxIncrease: 20,
							WarmupSteps: 1,
						},
					},
				})
				calc := NewIterationCalculator(ex)
				ec, ok := calc.(*exponentialCalculator)
				Expect(ok).To(BeTrue(), "expected *exponentialCalculator")
				Expect(ec.base).To(Equal(2.0))
			})
		})
	})

	Describe("linear pattern", func() {
		Context("when linear config block is missing", func() {
			It("should not panic and default to step=1", func() {
				ex := newTestExecutor(config.IncrementalLoad{
					StartIterations: 10,
					TotalIterations: 100,
					Pattern: config.LoadPattern{
						Type:   config.LinearPattern,
						Linear: nil,
					},
				})
				calc := NewIterationCalculator(ex)
				Expect(calc).NotTo(BeNil())

				lc, ok := calc.(*linearCalculator)
				Expect(ok).To(BeTrue(), "expected *linearCalculator")
				Expect(lc.step).To(Equal(1))
			})
		})

		Context("when linear config block is provided", func() {
			It("should use StepSize from config", func() {
				ex := newTestExecutor(config.IncrementalLoad{
					StartIterations: 10,
					TotalIterations: 100,
					Pattern: config.LoadPattern{
						Type: config.LinearPattern,
						Linear: &config.LinearLoadConfig{
							StepSize: 15,
						},
					},
				})
				calc := NewIterationCalculator(ex)
				lc, ok := calc.(*linearCalculator)
				Expect(ok).To(BeTrue(), "expected *linearCalculator")
				Expect(lc.step).To(Equal(15))
			})

			It("should adjust step when MinSteps requires more steps", func() {
				// startIt=10, totalIt=100 → range=90
				// StepSize=90 → totalSteps=1, MinSteps=5 → recalculate: step=ceil(90/5)=18
				ex := newTestExecutor(config.IncrementalLoad{
					StartIterations: 10,
					TotalIterations: 100,
					Pattern: config.LoadPattern{
						Type: config.LinearPattern,
						Linear: &config.LinearLoadConfig{
							StepSize: 90,
							MinSteps: 5,
						},
					},
				})
				calc := NewIterationCalculator(ex)
				lc, ok := calc.(*linearCalculator)
				Expect(ok).To(BeTrue(), "expected *linearCalculator")
				Expect(lc.step).To(Equal(18))
			})
		})
	})

	Describe("no pattern type set", func() {
		It("should default to linear behavior without panicking", func() {
			ex := newTestExecutor(config.IncrementalLoad{
				StartIterations: 5,
				TotalIterations: 50,
				Pattern:         config.LoadPattern{},
			})
			calc := NewIterationCalculator(ex)
			_, ok := calc.(*linearCalculator)
			Expect(ok).To(BeTrue(), "expected *linearCalculator for empty pattern type")
		})
	})
})
