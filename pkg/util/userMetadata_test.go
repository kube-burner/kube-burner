package util

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func createTempYAMLFile(t *testing.T, content string) string {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "user_metadata_*.yaml")
	assert.NoError(t, err)

	_, err = tmpFile.WriteString(content)
	assert.NoError(t, err)

	err = tmpFile.Close()
	assert.NoError(t, err)

	return tmpFile.Name()
}

func TestReadUserMetadata_Success(t *testing.T) {
	yamlContent := `
name: test-user
project: kube-burner
enabled: true
count: 5
tags:
  - perf
  - benchmark
`
	tmpFile := createTempYAMLFile(t, yamlContent)
	defer os.Remove(tmpFile)

	result, err := ReadUserMetadata(tmpFile)
	assert.NoError(t, err)

	assert.Equal(t, "test-user", result["name"])
	assert.Equal(t, "kube-burner", result["project"])
	assert.Equal(t, true, result["enabled"])
	assert.Equal(t, 5, result["count"])

	tags, ok := result["tags"].([]interface{})
	assert.True(t, ok)
	assert.Contains(t, tags, "perf")
	assert.Contains(t, tags, "benchmark")
}

func TestReadUserMetadata_FileNotFound(t *testing.T) {
	result, err := ReadUserMetadata("non_existent_file.yaml")
	assert.Error(t, err)
	assert.Empty(t, result)
}

func TestReadUserMetadata_InvalidYAML(t *testing.T) {
	invalidContent := `: invalid yaml content`
	tmpFile := createTempYAMLFile(t, invalidContent)
	defer os.Remove(tmpFile)

	result, err := ReadUserMetadata(tmpFile)
	assert.Error(t, err)
	assert.Empty(t, result)
}

func TestReadUserMetadata_EmptyYAML(t *testing.T) {
	emptyContent := ``
	tmpFile := createTempYAMLFile(t, emptyContent)
	defer os.Remove(tmpFile)

	result, err := ReadUserMetadata(tmpFile)

	// Accept io.EOF as a valid outcome for empty YAML files
	if err != nil && err.Error() != "EOF" {
		t.Errorf("Unexpected error: %v", err)
	}

	assert.Empty(t, result)
}
