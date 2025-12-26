package util_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kube-burner/kube-burner/pkg/util"
)

func TestCleanupTemplate(t *testing.T) {
	input := []byte("kind: Pod\nmetadata:\n  name: example\n  annotations:\n    description: '{{ .Description }}'\n")
	cleaned, err := util.CleanupTemplate(input)
	if err != nil {
		t.Fatalf("CleanupTemplate returned error: %v", err)
	}

	if string(cleaned) == string(input) {
		t.Fatalf("expected placeholders to be removed, got original content: %s", cleaned)
	}

	if _, err := util.CleanupTemplate([]byte("   \n\t")); err == nil {
		t.Fatalf("expected error for empty template, got nil")
	}
}

func TestRenderTemplate(t *testing.T) {
	templateContent := []byte("apiVersion: v1\nmetadata:\n  name: {{ .Name }}\n")
	rendered, err := util.RenderTemplate(templateContent, map[string]string{"Name": "test"}, util.MissingKeyError, nil)
	if err != nil {
		t.Fatalf("RenderTemplate returned error: %v", err)
	}

	expected := "apiVersion: v1\nmetadata:\n  name: test\n"
	if string(rendered) != expected {
		t.Fatalf("unexpected rendered template. want %q, got %q", expected, rendered)
	}

	_, err = util.RenderTemplate(templateContent, map[string]string{}, util.MissingKeyError, nil)
	if err == nil {
		t.Fatalf("expected error due to missing key, got nil")
	}
}

func TestRenderTemplateWithCustomFunction(t *testing.T) {
	util.AddRenderingFunction("Double", func(value int) int { return value * 2 })

	templateContent := []byte("result: {{ Double .Value }}\n")
	rendered, err := util.RenderTemplate(templateContent, map[string]int{"Value": 21}, util.MissingKeyError, nil)
	if err != nil {
		t.Fatalf("RenderTemplate with custom function returned error: %v", err)
	}

	expected := "result: 42\n"
	if string(rendered) != expected {
		t.Fatalf("unexpected rendered template. want %q, got %q", expected, rendered)
	}
}

func TestEnvToMap(t *testing.T) {
	t.Setenv("KUBE_BURNER_ENV_MAP", "value")
	envMap := util.EnvToMap()

	got, ok := envMap["KUBE_BURNER_ENV_MAP"].(string)
	if !ok || got != "value" {
		t.Fatalf("expected env map to contain key with value %q, got %v", "value", envMap["KUBE_BURNER_ENV_MAP"])
	}
}

func TestCreateFile(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "example.txt")
	content := []byte("hello world")

	if err := util.CreateFile(filePath, content); err != nil {
		t.Fatalf("CreateFile returned error: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read created file: %v", err)
	}

	if string(data) != string(content) {
		t.Fatalf("unexpected file contents. want %q, got %q", string(content), string(data))
	}
}
