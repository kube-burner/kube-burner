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

package util

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEnhanceYAMLParseError(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		err      error
		expected string
	}{
		{
			name:     "nil error",
			filename: "test.yml",
			err:      nil,
			expected: "",
		},
		{
			name:     "yaml.TypeError",
			filename: "config.yml",
			err:      &yaml.TypeError{Errors: []string{"line 5: invalid syntax", "line 10: unknown field"}},
			expected: "failed to parse config file config.yml: line 5: invalid syntax; line 10: unknown field",
		},
		{
			name:     "generic error with line info",
			filename: "test.yml",
			err:      yaml.Unmarshal([]byte("invalid: [unclosed"), &map[string]interface{}{}),
			expected: "failed to parse config file test.yml:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnhanceYAMLParseError(tt.filename, tt.err)
			if tt.err == nil {
				if result != nil {
					t.Errorf("Expected nil error, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected error, got nil")
				return
			}

			if !strings.Contains(result.Error(), tt.expected) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.expected, result.Error())
			}
		})
	}
}

func TestEnhanceJSONParseError(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		err      error
		expected string
	}{
		{
			name:     "nil error",
			filename: "test.json",
			err:      nil,
			expected: "",
		},
		{
			name:     "json.SyntaxError",
			filename: "config.json",
			err:      &json.SyntaxError{Offset: 42},
			expected: "failed to parse config file config.json: JSON syntax error at offset 42",
		},
		{
			name:     "json.UnmarshalTypeError",
			filename: "data.json",
			err:      &json.UnmarshalTypeError{Field: "name", Offset: 25, Type: nil, Value: "number"},
			expected: "failed to parse config file data.json: JSON type error at field 'name' (offset 25)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EnhanceJSONParseError(tt.filename, tt.err)
			if tt.err == nil {
				if result != nil {
					t.Errorf("Expected nil error, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected error, got nil")
				return
			}

			if !strings.Contains(result.Error(), tt.expected) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.expected, result.Error())
			}
		})
	}
}
