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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	log "github.com/sirupsen/logrus"
)

type hookResult struct {
	hook     config.Hook
	err      error
	duration time.Duration
	stdout   string
	stderr   string
}

type hookProcess struct {
	cmd       *exec.Cmd
	startTime time.Time
	mu        sync.RWMutex
	done      chan struct{}
	result    hookResult
}

type HookManager struct {
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	resultChan chan hookResult
}

// NewHookManager creates a new HookManager
func NewHookManager(ctx context.Context, configSpec config.Spec) *HookManager {
	ctx, cancel := context.WithCancel(ctx)
	channelSize := 0
	if len(configSpec.Jobs) > 0 {
		for _, job := range configSpec.Jobs {
			channelSize += len(job.Hooks)
		}
	}
	return &HookManager{
		ctx:        ctx,
		cancel:     cancel,
		resultChan: make(chan hookResult, channelSize),
	}
}

// executeHooks executes hooks based on the specified timing (when)
func (ex JobExecutor) executeHooks(when config.JobHook) error {
	if len(ex.Hooks) == 0 {
		return nil
	}

	var (
		foregroundWg  sync.WaitGroup
		errorInternal []error
	)
	// Channel to collect hook results
	resultChan := make(chan hookResult, len(ex.Hooks))

	// var backgroundProcesses []*hookProcess
	for _, hook := range ex.Hooks {
		if hook.When != when {
			continue
		}

		if len(hook.Cmd) == 0 {
			log.Warnf("Empty command for hook %s, skipping it", when)
			continue
		}
		if hook.Background {
			// Start in background, store process
			if err := ex.hookManager.executeBackgroundHook(hook); err != nil {
				return fmt.Errorf("failed to start background hook at '%s': %w", when, err)
			}
		} else {
			foregroundWg.Add(1)
			go func(h config.Hook) {
				defer foregroundWg.Done()
				if err := ex.hookManager.executeForegroundHook(h); err != nil {
					resultChan <- hookResult{
						hook: h,
						err:  err,
					}
				}
			}(hook)
		}
	}
	go func() {
		foregroundWg.Wait()
		close(resultChan)
	}()

	for err := range resultChan {
		errorInternal = append(errorInternal, err.err)
	}

	if len(errorInternal) > 0 {
		return fmt.Errorf("hook execution errorInternal at '%s': %v", when, errorInternal)
	}

	return nil
}

func (hm *HookManager) executeBackgroundHook(hook config.Hook) error {
	log.Infof("Starting Background hook at %s , %v", hook.When, hook.Cmd)
	cmd := exec.CommandContext(hm.ctx, hook.Cmd[0], hook.Cmd[1:]...)

	var stdout, stderr bytes.Buffer
	hp := &hookProcess{
		cmd:       cmd,
		startTime: time.Now(),
		done:      make(chan struct{}),
		result: hookResult{
			hook: hook,
		},
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set process group for proper cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start background hook: %w", err)
	}

	hm.wg.Add(1)
	go hm.monitorBackgroundHook(hp, &stdout, &stderr)

	return nil
}

// monitorBackgroundHook monitors a background hook with proper error handling
func (hm *HookManager) monitorBackgroundHook(hp *hookProcess, stdout, stderr *bytes.Buffer) {
	defer hm.wg.Done()
	defer close(hp.done)

	// Wait for process with timeout context
	errChan := make(chan error, 1)
	go func() {
		errChan <- hp.cmd.Wait()
	}()

	select {
	case err := <-errChan:
		hp.mu.Lock()
		hp.result.err = err
		hp.result.duration = time.Since(hp.startTime)
		hp.result.stdout = stdout.String()
		hp.result.stderr = stderr.String()
		result := hp.result

		hp.mu.Unlock()

		// Send result to channel (non-blocking)
		select {
		case hm.resultChan <- result:
		default:
			log.Warnf("Result channel full, logging directly")
		}

		if err != nil {
			log.Errorf("Background hook at '%s' failed after %v: %v", hp.result.hook.When, hp.result.duration, err)
			if result.stderr != "" {
				log.Errorf("Hook stderr: %s", result.stderr)
			}
		} else {
			log.Infof("Background hook at '%s' completed successfully in %v", hp.result.hook.When, hp.result.duration)
			if result.stdout != "" {
				log.Debugf("Hook stdout: %s", result.stdout)
			}
		}

	case <-hm.ctx.Done():
		log.Warnf("Hook monitor cancel for '%s'", hp.result.hook.When)
	}
}

func (hm *HookManager) executeForegroundHook(hook config.Hook) error {
	log.Infof("Executing foreground hook: %v", hook.Cmd)
	timeout := 5 * time.Minute
	ctx, cancel := context.WithTimeout(hm.ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, hook.Cmd[0], hook.Cmd[1:]...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	if stdout.Len() > 0 {
		log.Debugf("Hook stdout: %s", stdout.String())
	}
	if stderr.Len() > 0 {
		log.Debugf("Hook stderr: %s", stderr.String())
	}

	if err != nil {
		// Check if it was a timeout
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("hook timed out after %v at '%s': %w", timeout, hook.When, err)
		}
		return fmt.Errorf("hook failed at '%s' after %v: %w", hook.When, duration, err)
	}

	log.Infof("Hook completed at '%s' in %v", hook.When, duration)
	return nil
}

// GetBackgroundHookResults returns results from background hooks (non-blocking)
func (hm *HookManager) GetBackgroundHookResults() []hookResult {
	if hm == nil {
		return nil
	}

	var results []hookResult
	for {
		select {
		case result := <-hm.resultChan:
			results = append(results, result)
		default:
			return results
		}
	}
}
