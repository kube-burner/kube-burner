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

package util

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestCreateFile(t *testing.T) {
	fileName := filepath.Join(t.TempDir(), "workload.yml")
	fileContent := []byte("kind: Pod\n")

	if err := CreateFile(fileName, fileContent); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("unexpected error reading %s: %v", fileName, err)
	}
	if string(content) != string(fileContent) {
		t.Fatalf("file content is %q, want %q", content, fileContent)
	}
}

func TestCreateFileReturnsWriteError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full test is Linux-specific")
	}
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skipf("/dev/full is unavailable: %v", err)
	}
	err := CreateFile("/dev/full", []byte("kind: Pod\n"))
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("CreateFile error = %v, want ENOSPC", err)
	}
}
