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

const (
	HookBeforeDeployment = "beforeDeployment"
	HookAfterDeployment  = "afterDeployment"
	HookBeforeChurn      = "beforeChurn"
	HookAfterChurn       = "afterChurn"
	HookBeforeCleanup    = "beforeCleanup"
	HookAfterCleanup     = "afterCleanup"
	HookBeforeGC         = "beforeGC"
	HookAfterGC          = "afterGC"
	HookOnEachIteration  = "onEachIteration"
)

type hookResult struct {
	hook     config.Hook
	when     string
	err      error
	duration time.Duration
	stdout   string
	stderr   string
}

type hookProcess struct {
	cmd       *exec.Cmd
	hook      config.Hook
	when      string
	stdout    *bytes.Buffer
	stderr    *bytes.Buffer
	startTime time.Time
	mu        sync.RWMutex
	done      chan struct{}
}

type HookManager struct {
	backgroundHooks []*hookProcess
	mu              sync.RWMutex
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	resultChan      chan hookResult
}

func NewHookManager(ctx context.Context) *HookManager {
	ctx, cancel := context.WithCancel(ctx)

	return &HookManager{
		backgroundHooks: make([]*hookProcess, 0),
		ctx:             ctx,
		cancel:          cancel,
		resultChan:      make(chan hookResult, 100), // Buffered channel to avoid blocking
	}
}

func (ex JobExecutor) executeHooks(when string) error {
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

		if len(hook.CMD) == 0 {
			log.Warnf("Hook for %s has been empty command, skipping", when)
			continue
		}
		if hook.Background {
			// Start in background , store process
			if err := ex.executeBackgroundHook(hook, when); err != nil {
				// log.Errorf("Error executing background hook for %s: %v", when, err)
				errorsMu.Lock()
				_ = append(errorInternal, fmt.Errorf("failed to start background hook at '%s': %w", when, err))
				errorsMu.Unlock()
				return err
			}
		} else {
			// Execute and wait
			// if err := ex.executeForegroundHook(hook, when); err != nil {
			// 	log.Errorf("Error executing hook for %s: %v", when, err)
			// 	return err
			// }
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

func (ex *JobExecutor) executeBackgroundHook(hook config.Hook, when string) error {
	log.Infof("Starting Background hook at %s , %v", when, hook.CMD)
	cmd := exec.CommandContext(ex.hookManager.ctx, hook.CMD[0], hook.CMD[1:]...)

	hp := &hookProcess{
		cmd:       cmd,
		hook:      hook,
		when:      when,
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
		startTime: time.Now(),
		done:      make(chan struct{}),
	}
	cmd.Stdout = hp.stdout
	cmd.Stderr = hp.stderr
	cmd.Stderr = hp.stderr

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
	go ex.monitorBackgroundHook(hp)

	return nil
}

// monitorBackgroundHook monitors a background hook with proper error handling
func (ex *JobExecutor) monitorBackgroundHook(hp *hookProcess) {
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
		duration := time.Since(hp.startTime)

		result := hookResult{
			hook:     hp.hook,
			when:     hp.when,
			err:      err,
			duration: duration,
			stdout:   hp.stdout.String(),
		}
		hp.mu.Unlock()

		// Send result to channel (non-blocking)
		select {
		case ex.hookManager.resultChan <- result:
		default:
			log.Warnf("Result channel full, logging directly")
		}

		if err != nil {
			log.Errorf("❌ Background hook at '%s' failed after %v: %v", hp.when, duration, err)
			if result.stderr != "" {
				log.Errorf("Hook stderr: %s", result.stderr)
			}
		} else {
			log.Infof("✅ Background hook at '%s' completed successfully in %v", hp.when, duration)
			if result.stdout != "" {
				log.Debugf("Hook stdout: %s", result.stdout)
			}
		}

	case <-ex.hookManager.ctx.Done():
		log.Warnf("⚠️  Hook monitor cancel for '%s'", hp.when)
	}
}

func (ex *JobExecutor) executeForegroundHook(hook config.Hook, when string) error {
	log.Infof("Executing foreground hook at '%s': %v", when, hook.CMD)
	timeout := 5 * time.Minute
	ctx, cancel := context.WithTimeout(ex.hookManager.ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, hook.CMD[0], hook.CMD[1:]...)

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

	log.Infof("⏳ Waiting for %d background hooks to complete (timeout: %v)...", count, timeout)

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		ex.hookManager.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Infof("✅ All background hooks completed")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for background hooks after %v", timeout)
	}
}

// cleanupBackgroundHooks terminates all running background hooks gracefully
func (ex *JobExecutor) cleanupBackgroundHooks() {
	if ex.hookManager == nil {
		return
	}

	ex.hookManager.mu.Lock()
	defer ex.hookManager.mu.Unlock()

	if len(ex.hookManager.backgroundHooks) == 0 {
		return
	}

	log.Infof("🧹 Cleaning up %d background hooks...", len(ex.hookManager.backgroundHooks))

	// Cancel context to signal all monitors
	ex.hookManager.cancel()

	var cleanupWg sync.WaitGroup
	for _, hp := range ex.hookManager.backgroundHooks {
		cleanupWg.Add(1)
		go func(p *hookProcess) {
			defer cleanupWg.Done()
			ex.terminateHookProcess(p)
		}(hp)
	}

	// Wait for all cleanup goroutines with timeout
	cleanupDone := make(chan struct{})
	go func() {
		cleanupWg.Wait()
		close(cleanupDone)
	}()

	select {
	case <-cleanupDone:
		log.Info("✅ All background hooks cleaned up")
	case <-time.After(10 * time.Second):
		log.Warn("⚠️  Cleanup timeout, some hooks may still be running")
	}

	// Clear the slice
	ex.hookManager.backgroundHooks = ex.hookManager.backgroundHooks[:0]
}

func (ex *JobExecutor) terminateHookProcess(hp *hookProcess) {
	hp.mu.Lock()
	defer hp.mu.Unlock()

	if hp.cmd == nil || hp.cmd.Process == nil {
		return
	}

	pid := hp.cmd.Process.Pid
	log.Infof("Terminating hook process (PID: %d) for '%s'", pid, hp.when)

	// Try graceful shutdown first (SIGTERM)
	if err := hp.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Warnf("Failed to send SIGTERM to PID %d: %v", pid, err)
	}

	// Wait for graceful shutdown with timeout
	gracefulDone := make(chan struct{})
	go func() {
		select {
		case <-hp.done:
			close(gracefulDone)
		case <-time.After(5 * time.Second):
			// Force kill if graceful shutdown fails
			log.Warnf("Graceful shutdown timeout, force killing PID %d", pid)
			if killErr := hp.cmd.Process.Kill(); killErr != nil {
				log.Errorf("Failed to kill PID %d: %v", pid, killErr)
			}
			close(gracefulDone)
		}
	}()

	<-gracefulDone
	log.Debugf("Process %d terminated", pid)
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

func validateHooks(hooks []config.Hook) error {
	validWhen := map[string]bool{
		HookBeforeDeployment: true,
		HookAfterDeployment:  true,
		HookBeforeChurn:      true,
		HookAfterChurn:       true,
		HookBeforeCleanup:    true,
		HookAfterCleanup:     true,
		HookBeforeGC:         true,
		HookAfterGC:          true,
		HookOnEachIteration:  true,
	}

	for i, hook := range hooks {
		if !validWhen[hook.When] {
			return fmt.Errorf("hook %d has invalid 'when' value: %s (valid: %v)",
				i, hook.When, getValidWhenValues())
		}
		if len(hook.CMD) == 0 {
			return fmt.Errorf("hook %d has empty command", i)
		}
	}

	return nil
}
func getValidWhenValues() []string {
	return []string{
		HookBeforeDeployment,
		HookAfterDeployment,
		HookBeforeChurn,
		HookAfterChurn,
		HookBeforeCleanup,
		HookAfterCleanup,
		HookBeforeGC,
		HookAfterGC,
		HookOnEachIteration,
	}
}
