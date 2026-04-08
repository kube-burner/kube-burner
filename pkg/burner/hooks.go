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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
)

type hookResult struct {
	err      error
	duration time.Duration
}

type HookManager struct {
	ctx         context.Context
	cancel      context.CancelFunc
	resultChan  chan hookResult
	backgrounWg sync.WaitGroup
	embedCfg    *fileutils.EmbedConfiguration
}

// NewHookManager creates a new HookManager
func NewHookManager(ctx context.Context, hookCount int, embedCfg *fileutils.EmbedConfiguration) *HookManager {
	ctx, cancel := context.WithCancel(ctx)
	return &HookManager{
		ctx:         ctx,
		cancel:      cancel,
		resultChan:  make(chan hookResult, hookCount),
		backgrounWg: sync.WaitGroup{},
		embedCfg:    embedCfg,
	}
}

// executeHooks executes hooks based on the specified timing (when)
func (hm *HookManager) executeHooks(hooks []config.Hook, when config.JobHook) error {
	if len(hooks) == 0 {
		return nil
	}
	// Execute background hooks in parallel
	for _, hook := range hooks {
		if hook.When != when {
			continue
		}

		if hook.Background {
			// Start in background, store process
			if err := hm.executeBackgroundHook(hook); err != nil {
				return fmt.Errorf("failed to start background hook at '%s': %w", when, err)
			}
		}
	}
	// Execute foreground hooks sequentially
	for _, hook := range hooks {
		if hook.When != when {
			continue
		}
		if !hook.Background {
			if err := hm.executeForegroundHook(hook); err != nil {
				return fmt.Errorf("foreground hook execution failed at '%s': %w", when, err)
			}
		}
	}

	return nil
}

// isShellScript checks whether the command invokes sh or bash with a script file argument.
// It returns the script path for shell script invocations, or an empty string otherwise.
// Whether that script should be read from the embedded FS is determined later in prepareCommand().
func isShellScript(cmd []string) string {
	if len(cmd) < 2 {
		return ""
	}
	shell := filepath.Base(cmd[0])
	if shell != "bash" && shell != "sh" {
		return ""
	}
	script := cmd[1]
	// Check if it's a script file (not a flag like -c)
	if len(script) > 0 && script[0] == '-' {
		return ""
	}
	// Check for common script extensions or assume it's a script if no extension
	ext := filepath.Ext(script)
	if ext == ".sh" || ext == "" {
		return script
	}
	return ""
}

// prepareCommand creates an exec.Cmd, potentially reading the script from embedded FS
// It first checks if the script exists locally or is an absolute path, and only falls back
// to embedded FS if the script is not found
func (hm *HookManager) prepareCommand(hook config.Hook) (*exec.Cmd, io.ReadCloser, error) {
	var scriptReader io.ReadCloser
	var cmd *exec.Cmd

	scriptPath := isShellScript(hook.Cmd)
	if scriptPath != "" {
		// Check if script exists locally or is an absolute path
		useEmbedded := false
		if !filepath.IsAbs(scriptPath) {
			if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
				// Script not found locally, try embedded FS
				useEmbedded = true
			}
		}

		if useEmbedded && hm.embedCfg != nil {
			// Try to read script from embedded FS
			reader, err := fileutils.GetScriptsReader(scriptPath, hm.embedCfg)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read script %s: %w", scriptPath, err)
			}
			scriptReader = reader

			// Build command: shell -s - <args>
			// -s tells shell to read from stdin, - marks end of options
			args := []string{"-s", "-"}
			if len(hook.Cmd) > 2 {
				args = append(args, hook.Cmd[2:]...)
			}
			cmd = exec.CommandContext(hm.ctx, hook.Cmd[0], args...)
			cmd.Stdin = scriptReader
			log.Debugf("Reading script %s from embedded filesystem", scriptPath)
		} else {
			// Execute command directly (script exists locally or is absolute path)
			cmd = exec.CommandContext(hm.ctx, hook.Cmd[0], hook.Cmd[1:]...)
		}
	} else {
		// Not a shell script invocation, execute command directly
		cmd = exec.CommandContext(hm.ctx, hook.Cmd[0], hook.Cmd[1:]...)
	}

	return cmd, scriptReader, nil
}

func (hm *HookManager) executeBackgroundHook(hook config.Hook) error {
	log.Infof("Starting Background hook at %s , %v", hook.When, hook.Cmd)

	cmd, scriptReader, err := hm.prepareCommand(hook)
	if err != nil {
		return err
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set process group for proper cleanup on Unix systems
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		if scriptReader != nil {
			scriptReader.Close()
		}
		return fmt.Errorf("failed to start background hook: %w", err)
	}
	hm.backgrounWg.Add(1)
	go hm.monitorBackgroundHook(cmd, hook, time.Now(), &stdout, &stderr, scriptReader)

	return nil
}

// monitorBackgroundHook monitors a background hook with proper error handling
func (hm *HookManager) monitorBackgroundHook(cmd *exec.Cmd, hook config.Hook, startTime time.Time, stdout, stderr *bytes.Buffer, scriptReader io.ReadCloser) {
	defer hm.backgrounWg.Done()
	defer func() {
		if scriptReader != nil {
			scriptReader.Close()
		}
	}()
	// Wait for process with timeout context
	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Wait()
	}()

	select {
	case err := <-errChan:
		duration := time.Since(startTime)
		result := hookResult{
			err:      err,
			duration: duration,
		}

		// Send result to channel (non-blocking)
		select {
		case hm.resultChan <- result:
			// Result sent successfully
		default:
			if err != nil {
				log.Errorf("Background hook at '%s' failed after %v: %v", hook.When, duration, err)
				if stderr.String() != "" {
					log.Errorf("Hook stderr: %s", stderr.String())
				}
			} else {
				log.Infof("Background hook at '%s' completed successfully in %v", hook.When, duration)
				if stdout.String() != "" {
					log.Debugf("Hook stdout: %s", stdout.String())
				}
			}
		}

	case <-hm.ctx.Done():
		log.Warnf("Hook monitor cancel for '%s'", hook.When)
	}
}

func (hm *HookManager) executeForegroundHook(hook config.Hook) error {
	log.Infof("Executing foreground hook: %v", hook.Cmd)

	cmd, scriptReader, err := hm.prepareCommand(hook)
	if err != nil {
		return err
	}
	if scriptReader != nil {
		defer scriptReader.Close()
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	if stdout.Len() > 0 {
		log.Debugf("Hook stdout: %s", stdout.String())
	}
	if stderr.Len() > 0 {
		log.Debugf("Hook stderr: %s", stderr.String())
	}

	if err != nil {
		// Check if the command was canceled via the context
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(hm.ctx.Err(), context.Canceled) {
			return fmt.Errorf("hook canceled at '%s' after %v: %w", hook.When, duration, err)
		}
		return fmt.Errorf("hook failed at '%s' after %v: %w", hook.When, duration, err)
	}

	log.Infof("Hook completed at '%s' in %v", hook.When, duration)
	return nil
}

// WaitBackgroundHooks waits for all background hooks to complete and returns their results
func (hm *HookManager) WaitBackgroundHooks() []hookResult {
	hm.backgrounWg.Wait()

	// Drain results from the channel (non-blocking)
	var results []hookResult
	for {
		select {
		case result := <-hm.resultChan:
			results = append(results, result)
		default:
			// No more results, reset channel for future hooks and return
			hm.resultChan = make(chan hookResult, cap(hm.resultChan))
			return results
		}
	}
}
