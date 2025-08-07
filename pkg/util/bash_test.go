package util

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"github.com/stretchr/testify/assert"
)

func createTempShellScript(t *testing.T, content string) string {
	t.Helper()
	// createTempShellScript creates a temporary shell script file with provided content
	tmpFile, err := os.CreateTemp("", "test_script_*.sh")
	assert.NoError(t, err)

	_, err = tmpFile.WriteString(content)
	assert.NoError(t, err)

	err = tmpFile.Close()
	assert.NoError(t, err)

	if runtime.GOOS != "windows" {
		err = os.Chmod(tmpFile.Name(), 0755)
		assert.NoError(t, err)
	}

	return tmpFile.Name()
}

func TestRunShellCmd_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping shell tests on Windows")
	}

	script := createTempShellScript(t, `echo "Hello from shell"`)
	defer os.Remove(script)

	scriptPath := filepath.ToSlash(script)
	cmd := scriptPath

	stdout, stderr, err := RunShellCmd(cmd, &fileutils.EmbedConfiguration{})
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "Hello from shell")
	assert.Empty(t, stderr.String())
}

func TestRunShellCmd_WithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping shell tests on Windows")
	}

	script := createTempShellScript(t, `echo "Arg1: $1"`)
	defer os.Remove(script)

	scriptPath := filepath.ToSlash(script)
	cmd := scriptPath + " testValue"

	stdout, stderr, err := RunShellCmd(cmd, &fileutils.EmbedConfiguration{})
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "Arg1: testValue")
	assert.Empty(t, stderr.String())
}

func TestRunShellCmd_ScriptNotFound(t *testing.T) {
	cmd := "nonexistent_script.sh"

	_, _, err := RunShellCmd(cmd, nil)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "cannot find"))
}

func TestRunShellCmd_ScriptFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping shell tests on Windows")
	}

	script := createTempShellScript(t, `exit 1`)
	defer os.Remove(script)

	scriptPath := filepath.ToSlash(script)
	cmd := scriptPath

	stdout, stderr, err := RunShellCmd(cmd, &fileutils.EmbedConfiguration{})
	assert.Error(t, err)
	assert.NotNil(t, stdout)
	assert.NotNil(t, stderr)

	var exitErr *exec.ExitError
	assert.True(t, errors.As(err, &exitErr))
}
