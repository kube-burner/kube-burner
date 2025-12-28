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
	"fmt"
	"os/exec"
	"sync"
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

type hookProcess struct {
	cmd    *exec.Cmd
	hook   config.Hook
	when   string
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	stdin  *bytes.Buffer
	mu     sync.Mutex
}

func (ex JobExecutor) executeHooks(when string) error {
	// var backgroundProcesses []*hookProcess
	for _, hook := range ex.Hooks {
		if hook.When != when {

			if len(hook.CMD) == 0 {
				log.Warnf("Hook for %s has been empty command, skipping", when)
				continue
			}
			if hook.Background {
				// Start in background , store process
				if err := ex.executeBackgroundHook(hook, when); err != nil {
					log.Errorf("Error executing background hook for %s: %v", when, err)
					return err
				}
			} else {
				// Execute and wait
				if err := ex.executeForegroundHook(hook, when); err != nil {
					log.Errorf("Error executing hook for %s: %v", when, err)
					return err
				}
			}
		}
	}
	return nil
}

func (ex *JobExecutor) executeBackgroundHook(hook config.Hook, when string) error {
	log.Infof("Starting Background hook at %s , %v", when, hook.CMD)
	cmd := exec.Command(hook.CMD[0], hook.CMD[1:]...)

	hp := &hookProcess{
		cmd:    cmd,
		hook:   hook,
		when:   when,
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		stdin:  &bytes.Buffer{},
	}
	cmd.Stdout = hp.stdout
	cmd.Stderr = hp.stderr
	cmd.Stdin = hp.stdin
	cmd.Stderr = hp.stderr

	if err := cmd.Start(); err != nil {
		log.Errorf("failed to start background hook at %s , %v", when, err)
	}
	ex.backgroundHooks = append(ex.backgroundHooks, hp)

	// Monitor the process in background via goroutine
	go func() {
		if err := cmd.Wait(); err != nil {
			hp.mu.Lock()
			log.Errorf("Background hook at '%s' failed: %v", when, err)
			if hp.stderr.Len() > 0 {
				log.Errorf("Hook stderr: %s", hp.stderr.String())
			}
			hp.mu.Unlock()
		} else {
			log.Infof("Background hook at '%s' completed", when)
		}
	}()
	return nil
}

func (ex *JobExecutor) executeForegroundHook(hook config.Hook, when string) error {
	log.Infof("Executing foreground hook at '%s': %v", when, hook.CMD)

	cmd := exec.Command(hook.CMD[0], hook.CMD[1:]...)

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
		return fmt.Errorf("hook failed at '%s' after %v: %w", when, duration, err)
	}

	log.Infof("Hook completed at '%s' in %v", when, duration)
	return nil
}

func (ex *JobExecutor) cleanupBackgroundHooks() {
	if len(ex.backgroundHooks) == 0 {
		return
	}
	log.Infof("Cleaning up %d background hooks", len(ex.backgroundHooks))

	for _, hp := range ex.backgroundHooks {

		if hp.cmd.Process != nil {
			if err := hp.cmd.Process.Kill(); err != nil {
				log.Errorf("Failed to kill background hook at %s: %v", hp.when, err)
			}
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
			return fmt.Errorf("hook %d has invalid 'when' value: %s", i, hook.When)
		}
		if len(hook.CMD) == 0 {
			return fmt.Errorf("hook %d has empty command", i)
		}
	}

	return nil
}
