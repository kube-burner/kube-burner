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
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnhanceYAMLParseError enhances YAML parsing errors with more context
func EnhanceYAMLParseError(filename string, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's a yaml.TypeError which contains line/column info
	if yamlErr, ok := err.(*yaml.TypeError); ok {
		errorDetails := append([]string(nil), yamlErr.Errors...)
		return fmt.Errorf("failed to parse config file %s: %s", filename, strings.Join(errorDetails, "; "))
	}

	// For other YAML errors, try to extract line/column information
	errStr := err.Error()
	if strings.Contains(errStr, "line ") {
		return fmt.Errorf("failed to parse config file %s: %s", filename, errStr)
	}

	// Fallback for generic errors
	return fmt.Errorf("failed to parse config file %s: %s. Please ensure the file contains valid YAML or JSON", filename, errStr)
}

// EnhanceJSONParseError enhances JSON parsing errors with more context
func EnhanceJSONParseError(filename string, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's a json.SyntaxError which contains offset info
	if syntaxErr, ok := err.(*json.SyntaxError); ok {
		return fmt.Errorf("failed to parse config file %s: JSON syntax error at offset %d: %s", filename, syntaxErr.Offset, syntaxErr.Error())
	}

	// Check if it's a json.UnmarshalTypeError which contains field info
	if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
		return fmt.Errorf("failed to parse config file %s: JSON type error at field '%s' (offset %d): expected %s but got %s",
			filename, typeErr.Field, typeErr.Offset, typeErr.Type, typeErr.Value)
	}

	// For other JSON errors, try to extract line/column information
	errStr := err.Error()
	if strings.Contains(errStr, "line ") || strings.Contains(errStr, "offset") {
		return fmt.Errorf("failed to parse config file %s: %s", filename, errStr)
	}

	// Fallback for generic errors
	return fmt.Errorf("failed to parse config file %s: %s. Please ensure the file contains valid YAML or JSON", filename, errStr)
}
