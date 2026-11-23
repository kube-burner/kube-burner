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
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = write
	t.Cleanup(func() {
		os.Stderr = orig
		_ = write.Close()
		_ = read.Close()
	})
	fn()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, read); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestEmitHookFailureNoOpOnNilError(t *testing.T) {
	hook := config.Hook{When: config.BeforeCleanup, Cmd: []string{"./check.sh"}}
	out := captureStderr(t, func() {
		emitHookFailure(hook, "stdout", "stderr", "", nil)
	})
	if out != "" {
		t.Fatalf("expected no stderr, got %q", out)
	}
}

func TestEmitHookFailureStderrAndStdout(t *testing.T) {
	hook := config.Hook{
		When: config.BeforeJobExecution,
		Cmd:  []string{"./hook.sh", "arg"},
	}
	runErr := errors.New("exit status 1")
	out := captureStderr(t, func() {
		emitHookFailure(hook, " line out ", " hook failed ", "", runErr)
	})
	if !strings.Contains(out, "kube-burner hook failure when=beforeJobExecution") {
		t.Fatalf("missing label, got %q", out)
	}
	if !strings.Contains(out, "hook failed") {
		t.Fatalf("missing stderr body, got %q", out)
	}
	if !strings.Contains(out, "stdout:") || !strings.Contains(out, "line out") {
		t.Fatalf("missing stdout section, got %q", out)
	}
}

func TestEmitHookFailureOutputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.out")
	if err := os.WriteFile(path, []byte("file diagnostics\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hook := config.Hook{When: config.BeforeCleanup, Cmd: []string{"./x.sh"}, OutputFile: path}
	out := captureStderr(t, func() {
		emitHookFailure(hook, "", "", path, errors.New("failed"))
	})
	if !strings.Contains(out, "output-file "+path) {
		t.Fatalf("missing output-file path, got %q", out)
	}
	if !strings.Contains(out, "file diagnostics") {
		t.Fatalf("missing file contents, got %q", out)
	}
}

func TestEmitHookFailureErrorOnly(t *testing.T) {
	hook := config.Hook{When: config.AfterJobExecution, Cmd: []string{"true"}}
	runErr := errors.New("signal killed")
	out := captureStderr(t, func() {
		emitHookFailure(hook, "", "", "", runErr)
	})
	if !strings.Contains(out, "signal killed") {
		t.Fatalf("expected run error on stderr, got %q", out)
	}
}

func TestSanitizeHookLogText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "invalid utf8",
			in:   "ok" + string([]byte{0xff}) + "tail",
			want: "ok\uFFFDtail",
		},
		{
			name: "ansi stripped",
			in:   "\x1b[31mred\x1b[0m",
			want: "red",
		},
		{
			name: "control to space",
			in:   "a\x00\x01b",
			want: "a  b",
		},
		{
			name: "keeps newline",
			in:   "line1\nline2",
			want: "line1\nline2",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeHookLogText(tt.in); got != tt.want {
				t.Fatalf("sanitizeHookLogText() = %q, want %q", got, tt.want)
			}
		})
	}
	long := strings.Repeat("x", hookFailureOutputLimit+10)
	if got := sanitizeHookLogText(long); !strings.HasSuffix(got, "... (hook output truncated)") {
		t.Fatalf("expected truncation suffix, len=%d", len(got))
	}
}

func TestEmitHookFailureSanitizesStderr(t *testing.T) {
	hook := config.Hook{When: config.BeforeCleanup, Cmd: []string{"t"}}
	out := captureStderr(t, func() {
		emitHookFailure(hook, "", "\x1b[1mboom\x1b[0m\x00", "", errors.New("fail"))
	})
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x00") {
		t.Fatalf("unsanitized output leaked: %q", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected sanitized text, got %q", out)
	}
}

func TestHookStderrSinkTeesToBuffer(t *testing.T) {
	var buf bytes.Buffer
	var dst bytes.Buffer
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig; _ = r.Close() })
	sink := hookStderrSink(&buf)
	if _, err := sink.Write([]byte("tee-me\n")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "tee-me" {
		t.Fatalf("buffer got %q", buf.String())
	}
	_ = w.Close()
	if _, err := io.Copy(&dst, r); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "tee-me" {
		t.Fatalf("stderr got %q", dst.String())
	}
}

func TestHookStderrSinkSanitizesAndBoundsOutput(t *testing.T) {
	var buf bytes.Buffer
	orig := os.Stderr
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = write
	t.Cleanup(func() { os.Stderr = orig; _ = read.Close() })
	sink := hookStderrSink(&buf)
	if _, err := sink.Write([]byte("\x1b[31m" + strings.Repeat("x", hookFailureOutputLimit+100) + "\x1b[0m")); err != nil {
		t.Fatal(err)
	}
	_ = write.Close()
	var streamed bytes.Buffer
	if _, err := io.Copy(&streamed, read); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(streamed.String(), "\x1b") || len(streamed.String()) > hookFailureOutputLimit {
		t.Fatalf("unsafe or oversized stream: len=%d", streamed.Len())
	}
	if buf.String() != streamed.String() {
		t.Fatal("stream and buffer differ")
	}
}

func TestTruncateHookTextPreservesUTF8(t *testing.T) {
	s := strings.Repeat("a", hookFailureOutputLimit-1) + "é"
	got := truncateHookText(s, hookFailureOutputLimit)
	if !utf8.ValidString(got) {
		t.Fatal("truncation produced invalid UTF-8")
	}
}
