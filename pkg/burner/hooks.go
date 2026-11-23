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
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
)

type hookResult struct {
	err      error
	duration time.Duration
	hook     config.Hook
	stderr   string
}

// hookFailureOutputLimit matches the harness subprocess stderr capture window (32 KiB).
const hookFailureOutputLimit = 32 << 10

var ansiEscapeSequence = regexp.MustCompile(`\x1b\[[0-9;]*[ -/]*[@-~]`)

type hookStderrWriter struct {
	dst       io.Writer
	buf       *bytes.Buffer
	pending   []byte
	written   int
	truncated bool
}

// hookStderrSink forwards hook stderr to the process stderr while buffering for failure logs.
func hookStderrSink(buf *bytes.Buffer) io.Writer {
	return &hookStderrWriter{dst: os.Stderr, buf: buf}
}

func (w *hookStderrWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	data, tail := splitHookStreamTail(w.pending)
	w.pending = tail
	text := sanitizeHookStreamText(string(data))
	if w.truncated || text == "" {
		return len(p), nil
	}
	remaining := hookFailureOutputLimit - w.written
	if len(text) > remaining {
		const suffix = "\n... (hook output truncated)"
		if remaining <= 0 {
			w.truncated = true
			return len(p), nil
		}
		if remaining < len(suffix) {
			text = truncateUTF8(text, remaining)
			w.truncated = true
		} else {
			budget := remaining - len(suffix)
			if budget < 0 {
				budget = 0
			}
			text = truncateUTF8(text, budget) + suffix
			w.truncated = true
		}
	}
	if text == "" {
		return len(p), nil
	}
	if _, err := io.MultiWriter(w.dst, w.buf).Write([]byte(text)); err != nil {
		return 0, err
	}
	w.written += len(text)
	return len(p), nil
}

func splitHookStreamTail(data []byte) ([]byte, []byte) {
	validEnd := 0
	for validEnd < len(data) && utf8.FullRune(data[validEnd:]) {
		_, size := utf8.DecodeRune(data[validEnd:])
		validEnd += size
	}
	data, tail := data[:validEnd], data[validEnd:]
	if esc := bytes.LastIndexByte(data, '\x1b'); esc >= 0 {
		sequence := data[esc:]
		if len(sequence) > 1 && sequence[1] == '[' {
			complete := false
			for _, r := range sequence[2:] {
				if r >= '@' && r <= '~' {
					complete = true
					break
				}
			}
			if !complete && len(sequence)+len(tail) <= hookFailureOutputLimit {
				tail = append(append([]byte(nil), sequence...), tail...)
				data = data[:esc]
			}
		}
	}
	return data, tail
}

func (w *hookStderrWriter) Flush() error {
	text := sanitizeHookStreamText(string(w.pending))
	w.pending = nil
	if text == "" {
		return nil
	}
	remaining := hookFailureOutputLimit - w.written
	if remaining <= 0 {
		return nil
	}
	if len(text) > remaining {
		text = truncateUTF8(text, remaining)
		w.truncated = true
	}
	_, err := io.MultiWriter(w.dst, w.buf).Write([]byte(text))
	w.written += len(text)
	return err
}

func sanitizeHookStreamText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = ansiEscapeSequence.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		if r < utf8.RuneSelf && (r < ' ' || r == 0x7f) && r != '\n' && r != '\r' && r != '\t' {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeHookLogText makes hook output safe to print on stderr for log collectors:
// valid UTF-8, no terminal escape sequences, no C0 control bytes (except \n, \r, \t).
func sanitizeHookLogText(s string) string {
	if s == "" {
		return s
	}
	s = sanitizeHookText(s)
	return truncateHookText(s, hookFailureOutputLimit)
}

func sanitizeHookText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = ansiEscapeSequence.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t':
			b.WriteRune(r)
		default:
			if r < utf8.RuneSelf && (r < ' ' || r == 0x7f) {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func truncateHookText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const suffix = "\n... (hook output truncated)"
	if limit <= len(suffix) {
		return suffix[:limit]
	}
	return truncateUTF8(s, limit-len(suffix)) + suffix
}

func truncateUTF8(s string, limit int) string {
	cut := limit
	if cut > len(s) {
		cut = len(s)
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func emitHookFailure(hook config.Hook, stdout, stderr, outputPath string, runErr error) {
	if runErr == nil {
		return
	}
	label := fmt.Sprintf(
		"kube-burner hook failure when=%s cmd=%s", hook.When,
		strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(sanitizeHookLogText(fmt.Sprint(hook.Cmd))),
	)
	stderr = sanitizeHookLogText(stderr)
	stdout = sanitizeHookLogText(stdout)
	if stderr != "" {
		fmt.Fprintf(os.Stderr, "%s:\n%s\n", label, stderr)
	}
	if stdout != "" {
		fmt.Fprintf(os.Stderr, "%s stdout:\n%s\n", label, stdout)
	}
	if outputPath != "" {
		if info, err := os.Stat(outputPath); err == nil && info.Mode().IsRegular() {
			if f, err := os.Open(outputPath); err == nil {
				defer f.Close()
				if payload, err := io.ReadAll(io.LimitReader(f, hookFailureOutputLimit+1)); err == nil {
					if text := sanitizeHookLogText(string(payload)); text != "" {
						fmt.Fprintf(os.Stderr, "%s output-file %s:\n%s\n", label, strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(sanitizeHookLogText(outputPath)), text)
					}
				}
			}
		}
	}
	if stderr == "" && stdout == "" {
		fmt.Fprintf(os.Stderr, "%s: %v\n", label, runErr)
	}
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
func (hm *HookManager) executeHooks(hooks []config.Hook, when config.JobStage) error {
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

// prepareCommand creates an exec.Cmd, potentially reading the script from embedded FS
// It first checks if the script exists locally or is an absolute path, and only falls back
// to embedded FS if the script is not found
func (hm *HookManager) prepareCommand(hook config.Hook) (*exec.Cmd, io.ReadCloser, error) {
	var scriptReader io.ReadCloser
	var cmd *exec.Cmd
	scriptPath := hook.Cmd[0]
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

		// Build command: bash -s - <args...>
		// Script is read from stdin; positional params start at hook.Cmd[1].
		args := []string{"-s", "-"}
		if len(hook.Cmd) > 1 {
			args = append(args, hook.Cmd[1:]...)
		}
		cmd = exec.CommandContext(hm.ctx, "/bin/bash", args...)
		cmd.Stdin = scriptReader
		log.Debugf("Reading script %s from embedded filesystem", scriptPath)

	} else {
		cmd = exec.CommandContext(hm.ctx, hook.Cmd[0], hook.Cmd[1:]...)
	}
	return cmd, scriptReader, nil
}

func openHookOutputFile(path string) (*os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file %q: %w", path, err)
	}
	return f, nil
}

func (hm *HookManager) executeBackgroundHook(hook config.Hook) error {
	var stdout, stderr bytes.Buffer
	log.Infof("Starting Background hook at %s , %v", hook.When, hook.Cmd)
	cmd, scriptReader, err := hm.prepareCommand(hook)
	if err != nil {
		return err
	}

	var outputFile *os.File
	var stderrSink *hookStderrWriter
	if hook.OutputFile != "" {
		outputFile, err = openHookOutputFile(hook.OutputFile)
		if err != nil {
			if scriptReader != nil {
				scriptReader.Close()
			}
			return err
		}
		cmd.Stdout = outputFile
		stderrSink = hookStderrSink(&stderr).(*hookStderrWriter)
		cmd.Stderr = io.MultiWriter(outputFile, stderrSink)
	} else {
		cmd.Stdout = &stdout
		stderrSink = hookStderrSink(&stderr).(*hookStderrWriter)
		cmd.Stderr = stderrSink
	}

	// Set process group for proper cleanup on Unix systems
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		if outputFile != nil {
			outputFile.Close()
		}
		if scriptReader != nil {
			scriptReader.Close()
		}
		return fmt.Errorf("failed to start background hook: %w", err)
	}
	hm.backgrounWg.Add(1)
	go hm.monitorBackgroundHook(cmd, hook, time.Now(), &stdout, &stderr, scriptReader, outputFile, stderrSink)

	return nil
}

// monitorBackgroundHook monitors a background hook with proper error handling
func (hm *HookManager) monitorBackgroundHook(cmd *exec.Cmd, hook config.Hook, startTime time.Time, stdout, stderr *bytes.Buffer, scriptReader io.ReadCloser, outputFile *os.File, stderrSink *hookStderrWriter) {
	defer hm.backgrounWg.Done()
	defer func() {
		if outputFile != nil {
			outputFile.Close()
		}
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
		if stderrSink != nil {
			_ = stderrSink.Flush()
		}
		if err != nil {
			emitHookFailure(hook, stdout.String(), stderr.String(), hook.OutputFile, err)
		}
		result := hookResult{
			err:      err,
			duration: duration,
			hook:     hook,
			stderr:   stderr.String(),
		}

		// Send result to channel (non-blocking)
		select {
		case hm.resultChan <- result:
		// Result sent successfully
		default:
			if err != nil {
				log.Errorf("Background hook at '%s' failed after %v: %v", hook.When, duration, err)
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

	var stdout, stderr bytes.Buffer
	var outputFile *os.File
	if hook.OutputFile != "" {
		outputFile, err = openHookOutputFile(hook.OutputFile)
		if err != nil {
			if scriptReader != nil {
				scriptReader.Close()
			}
			return err
		}
		defer outputFile.Close()
		cmd.Stdout = outputFile
		cmd.Stderr = outputFile
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = hookStderrSink(&stderr)
	}

	if scriptReader != nil {
		defer scriptReader.Close()
	}

	start := time.Now()
	err = cmd.Run()
	if sink, ok := cmd.Stderr.(*hookStderrWriter); ok {
		_ = sink.Flush()
	}
	duration := time.Since(start)

	if outputFile == nil {
		if stdout.Len() > 0 {
			log.Debugf("Hook stdout: %s", stdout.String())
		}
		if stderr.Len() > 0 {
			log.Debugf("Hook stderr: %s", stderr.String())
		}
	}

	if err != nil {
		emitHookFailure(hook, stdout.String(), stderr.String(), hook.OutputFile, err)
		// Check if the command was canceled via the context
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(hm.ctx.Err(), context.Canceled) {
			return fmt.Errorf("hook canceled at %q after %v: %w", hook.When, duration, err)
		}
		return fmt.Errorf("hook failed at %q after %v: %w", hook.When, duration, err)
	}

	log.Infof("Hook completed at %q in %v", hook.When, duration)
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
