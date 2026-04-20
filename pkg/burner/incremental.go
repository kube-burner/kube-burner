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
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/cloud-bulldozer/go-commons/v2/indexers"
	"github.com/google/uuid"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements"
	"github.com/kube-burner/kube-burner/v2/pkg/prometheus"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	"github.com/kube-burner/kube-burner/v2/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
)

// IterationCalculator returns the next iteration window for incremental runs.
type IterationCalculator interface {
	Next(current int) (start, end int, done bool)
}

// NewIterationCalculator returns an IterationCalculator based on job's IncrementalLoad config.
func NewIterationCalculator(ex JobExecutor) IterationCalculator {
	cfg := ex.IncrementalLoad
	startIt := cfg.StartIterations
	if startIt <= 0 {
		startIt = ex.JobIterations
	}
	totalIt := cfg.TotalIterations
	if totalIt < startIt {
		totalIt = startIt
	}
	if totalIt <= 0 {
		return &linearCalculator{start: 0, total: 0, step: 0}
	}
	if cfg.Pattern.Type == config.ExponentialPattern {
		base := 2.0
		var maxInc, warmup int
		if cfg.Pattern.Exponential != nil {
			if cfg.Pattern.Exponential.Base > 0 {
				base = cfg.Pattern.Exponential.Base
			}
			maxInc = cfg.Pattern.Exponential.MaxIncrease
			warmup = cfg.Pattern.Exponential.WarmupSteps
		}
		return &exponentialCalculator{start: startIt, total: totalIt, base: base, maxIncrease: maxInc, warmup: warmup, stepNo: 0}
	} else {
		step := 1
		if cfg.Pattern.Linear != nil {
			if cfg.Pattern.Linear.StepSize > 0 {
				step = cfg.Pattern.Linear.StepSize
			}
			totalSteps := int(math.Ceil(float64(totalIt-startIt) / float64(step)))
			if cfg.Pattern.Linear.MinSteps > 0 && totalSteps < cfg.Pattern.Linear.MinSteps {
				remaining := totalIt - startIt
				if remaining <= 0 {
					step = totalIt
				} else {
					step = int(math.Ceil(float64(remaining) / float64(cfg.Pattern.Linear.MinSteps)))
				}
			}
		}
		if step <= 0 {
			step = 1
		}
		return &linearCalculator{start: startIt, total: totalIt, step: step}
	}
}

// linearCalculator increments iterations by a fixed step until total is reached.
type linearCalculator struct {
	start int
	total int
	step  int
}

func (l *linearCalculator) Next(current int) (start, end int, done bool) {
	if l.total <= 0 || current == l.total {
		return 0, 0, true
	}
	// first step: create start iterations
	if current == 0 {
		next := l.start
		if next > l.total {
			next = l.total
		}
		return 0, next, false
	}
	next := current + l.step
	if next > l.total {
		next = l.total
	}
	return current, next, false
}

// exponentialCalculator increases iterations exponentially after an optional warmup.
type exponentialCalculator struct {
	start       int
	total       int
	base        float64
	maxIncrease int
	warmup      int
	stepNo      int
}

func (e *exponentialCalculator) Next(current int) (start, end int, done bool) {
	if e.total <= 0 || current == e.total {
		return 0, 0, true
	}
	if current == 0 {
		e.stepNo = 1
		next := e.start
		if next > e.total {
			next = e.total
		}
		return 0, next, false
	}
	e.stepNo++
	if e.warmup > 0 && e.stepNo <= e.warmup {
		candidate := current + e.start
		if candidate > e.total {
			candidate = e.total
		}
		return current, candidate, false
	}
	expPower := float64(e.stepNo)
	if e.warmup > 0 {
		expPower = float64(e.stepNo - e.warmup)
	}
	increase := int(math.Pow(e.base, expPower)) * e.start
	if increase <= 0 {
		increase = 1
	}
	if e.maxIncrease > 0 && increase > e.maxIncrease {
		increase = e.maxIncrease
	}
	candidate := current + increase
	if candidate > e.total {
		candidate = e.total
	}
	return current, candidate, false
}

// RunIncrementalCreateJob executes incremental steps using the provided calculator.
func (ex *JobExecutor) RunIncrementalCreateJob(
	ctx context.Context,
	calculator IterationCalculator,
	msFactory *measurements.MeasurementsFactory,
	kubeClientProvider *config.KubeClientProvider,
	embedCfg *fileutils.EmbedConfiguration,
	measurementsJobName string,
	metricsScraper metrics.Scraper,
	configSpec config.Spec,
) ([]error, []prometheus.Job) {

	current := 0
	stepDelay := ex.IncrementalLoad.StepDelay
	var allErrs []error
	var stepJobs []prometheus.Job
	var startOperations, endOperations int32

	originalUUID := ex.uuid
	originalRunID := ex.runid
	originalIterations := ex.JobIterations

	for {
		_, end, done := calculator.Next(current)
		if done {
			log.Infof("Incremental creation completed")
			// Restore original UUID and run_id
			ex.uuid = originalUUID
			ex.runid = originalRunID
			ex.JobIterations = originalIterations
			configSpec.GlobalConfig.UUID = originalUUID
			return allErrs, stepJobs
		}

		log.Infof("Running incremental cumulative step: total iterations = %d", end)

		// Generate unique step-specific run_id for measurements and incremental load marker
		stepRunID := uuid.New().String()
		ex.runid = stepRunID
		log.Infof("Incremental Load UUID: %s (UUID: %s), Run_ID: %s", stepRunID, originalUUID, stepRunID)
		ex.JobIterations = end

		// create measurements specific for this incremental step using benchmark UUID and run_id
		labelSelector := fmt.Sprintf("%s=%s,%s=%s", config.KubeBurnerLabelRunID, stepRunID, config.KubeBurnerLabelUUID, originalUUID)

		// create step-specific metadata with incrementalLoadUUID marker
		stepMetadata := make(map[string]any)
		for k, v := range metricsScraper.SummaryMetadata {
			stepMetadata[k] = v
		}
		stepMetadata["incrementalLoadUUID"] = stepRunID
		startOperations = ex.objectOperations

		// create step-specific measurements factory with incrementalLoadUUID in metadata
		stepMsFactory := measurements.NewMeasurementsFactory(configSpec, stepMetadata, nil)
		msInstance := stepMsFactory.NewMeasurements(&ex.Job, kubeClientProvider, embedCfg, labelSelector)
		msInstance.Start()

		stepStart := time.Now().UTC()

		// For cumulative behavior, always create from 0 to end (so total becomes end)
		if errs := ex.RunCreateJob(ctx, 0, end); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
			if err := msInstance.Stop(); err != nil {
				allErrs = append(allErrs, err)
			}
			return allErrs, stepJobs
		}

		if ex.VerifyObjects && !ex.Verify(ctx, &end) {
			err := errors.New("object verification failed at total iterations: " + fmt.Sprint(end))
			if ex.ErrorOnVerify {
				allErrs = append(allErrs, err)
			}
			log.Error(err.Error())
			return allErrs, stepJobs
		}

		// Health check
		if script := ex.IncrementalLoad.HealthCheckScript; script != "" {
			out, errOut, err := util.RunShellCmd(script, ex.embedCfg)
			if out != nil {
				log.Debugf("Health check script stdout: %s", out.String())
			}
			if errOut != nil {
				log.Debugf("Health check script stderr: %s", errOut.String())
			}
			if err != nil {
				stderr := ""
				if errOut != nil {
					stderr = errOut.String()
				}
				allErrs = append(allErrs, fmt.Errorf("health check script failed: %v; stderr: %s", err, stderr))
				if err := msInstance.Stop(); err != nil {
					allErrs = append(allErrs, err)
				}
				return allErrs, stepJobs
			}
			log.Info("Cluster health check script succeeded, proceeding to next step")
		} else {
			util.ClusterHealthCheck(ex.clientSet)
			log.Info("Proceeding to the next step")
		}

		// stop measurements for this step and index them
		if err := msInstance.Stop(); err != nil {
			allErrs = append(allErrs, err)
		}
		if !ex.SkipIndexing && len(metricsScraper.IndexerList) > 0 {
			localIndexers, remoteIndexers := splitIndexersByType(configSpec, metricsScraper.IndexerList)
			if len(localIndexers) > 0 {
				msInstance.Index(measurementsJobName, localIndexers)
			}
			if len(remoteIndexers) > 0 {
				msInstance.Index(measurementsJobName, remoteIndexers)
			}
		}

		log.Infof("Running garbage collection for job %s (uuid=%s) after incremental step", ex.Name, ex.uuid)
		ex.gc(ctx, nil)
		endOperations = ex.objectOperations

		stepEnd := time.Now().UTC()
		stepJobs = append(stepJobs, prometheus.Job{
			Start:               stepStart,
			End:                 stepEnd,
			JobConfig:           ex.Job,
			ObjectOperations:    endOperations - startOperations,
			UUID:                originalUUID,
			IncrementalLoadUUID: stepRunID,
		})

		if stepDelay > 0 {
			log.Infof("Sleeping %v before next step", stepDelay)
			time.Sleep(stepDelay)
		}

		current = end
	}
}

// splitIndexersByType splits the configured indexers into local and remote maps.
func splitIndexersByType(configSpec config.Spec, indexerList map[string]indexers.Indexer) (map[string]indexers.Indexer, map[string]indexers.Indexer) {
	localIndexers := make(map[string]indexers.Indexer)
	remoteIndexers := make(map[string]indexers.Indexer)
	// Build sets of local and remote aliases from configuration
	for i, endpoint := range configSpec.MetricsEndpoints {
		alias := endpoint.Alias
		if alias == "" {
			alias = fmt.Sprintf("indexer-%d", i)
		}
		if indexer, exists := indexerList[alias]; exists {
			if endpoint.Type == indexers.LocalIndexer {
				localIndexers["collected-metrics"] = indexer
			} else {
				remoteIndexers[alias] = indexer
			}
		}
	}
	return localIndexers, remoteIndexers
}
