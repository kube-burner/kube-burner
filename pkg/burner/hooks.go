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
	when     config.JobHook
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
	backgroundHooks []*hookProcess
	mu              sync.RWMutex
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	resultChan      chan hookResult
}

// NewHookManager creates a new HookManager
func NewHookManager(ctx context.Context) *HookManager {
	ctx, cancel := context.WithCancel(ctx)

	return &HookManager{
		backgroundHooks: make([]*hookProcess, 0),
		ctx:             ctx,
		cancel:          cancel,
		resultChan:      make(chan hookResult),
	}
}

func (ex JobExecutor) executeHooks(when config.JobHook) error {
	if len(ex.Hooks) == 0 {
		return nil
	}

	var (
		foregroundWg  sync.WaitGroup
		errorsMu      sync.Mutex
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
			if err := ex.executeBackgroundHook(hook, when); err != nil {
				return fmt.Errorf("failed to start background hook at '%s': %w", when, err)
			}
		} else {
			foregroundWg.Add(1)
			go func(h config.Hook) {
				defer foregroundWg.Done()
				if err := ex.executeForegroundHook(h, when); err != nil {
					resultChan <- hookResult{
						hook: h,
						when: when,
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
		errorsMu.Lock()
		errorInternal = append(errorInternal, err.err)
		errorsMu.Unlock()
	}

	if len(errorInternal) > 0 {
		return fmt.Errorf("hook execution errorInternal at '%s': %v", when, errorInternal)
	}

	return nil
}

func (ex *JobExecutor) executeBackgroundHook(hook config.Hook, when config.JobHook) error {
	log.Infof("Starting Background hook at %s , %v", when, hook.Cmd)
	cmd := exec.CommandContext(ex.hookManager.ctx, hook.Cmd[0], hook.Cmd[1:]...)

	var stdout, stderr bytes.Buffer
	hp := &hookProcess{
		cmd:       cmd,
		startTime: time.Now(),
		done:      make(chan struct{}),
		result: hookResult{
			hook: hook,
			when: when,
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

	ex.hookManager.mu.Lock()
	ex.hookManager.backgroundHooks = append(ex.hookManager.backgroundHooks, hp)
	ex.hookManager.mu.Unlock()

	ex.hookManager.wg.Add(1)
	go ex.monitorBackgroundHook(hp, &stdout, &stderr)

	return nil
}

// monitorBackgroundHook monitors a background hook with proper error handling
func (ex *JobExecutor) monitorBackgroundHook(hp *hookProcess, stdout, stderr *bytes.Buffer) {
	defer ex.hookManager.wg.Done()
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
		case ex.hookManager.resultChan <- result:
		default:
			log.Warnf("Result channel full, logging directly")
		}

		if err != nil {
			log.Errorf("Background hook at '%s' failed after %v: %v", hp.result.when, hp.result.duration, err)
			if result.stderr != "" {
				log.Errorf("Hook stderr: %s", result.stderr)
			}
		} else {
			log.Infof("Background hook at '%s' completed successfully in %v", hp.result.when, hp.result.duration)
			if result.stdout != "" {
				log.Debugf("Hook stdout: %s", result.stdout)
			}
		}

	case <-ex.hookManager.ctx.Done():
		log.Warnf("Hook monitor cancel for '%s'", hp.result.when)
	}
}

func (ex *JobExecutor) executeForegroundHook(hook config.Hook, when config.JobHook) error {
	log.Infof("Executing foreground hook at '%s': %v", when, hook.Cmd)
	timeout := 5 * time.Minute
	ctx, cancel := context.WithTimeout(ex.hookManager.ctx, timeout)
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
			return fmt.Errorf("hook timed out after %v at '%s': %w", timeout, when, err)
		}
		return fmt.Errorf("hook failed at '%s' after %v: %w", when, duration, err)
	}

	log.Infof("Hook completed at '%s' in %v", when, duration)
	return nil
}

// WaitForBackgroundHooks waits for all background hooks to complete with timeout
func (ex *JobExecutor) WaitForBackgroundHooks(timeout time.Duration) error {
	if ex.hookManager == nil {
		return nil
	}

	ex.hookManager.mu.RLock()
	count := len(ex.hookManager.backgroundHooks)
	ex.hookManager.mu.RUnlock()

	if count == 0 {
		return nil
	}

	log.Infof("Waiting for %d background hooks to complete (timeout: %v)...", count, timeout)

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		ex.hookManager.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Infof("All background hooks completed")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for background hooks after %v", timeout)
	}
}

// GetBackgroundHookResults returns results from background hooks (non-blocking)
func (ex *JobExecutor) GetBackgroundHookResults() []hookResult {
	if ex.hookManager == nil {
		return nil
	}

	var results []hookResult
	for {
		select {
		case result := <-ex.hookManager.resultChan:
			results = append(results, result)
		default:
			return results
		}
	}
}
