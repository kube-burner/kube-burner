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
	"os"

	"gopkg.in/yaml.v3"
)

// OutputFormat represents the supported output formats
type OutputFormat string

const (
	OutputFormatJSON OutputFormat = "json"
	OutputFormatYAML OutputFormat = "yaml"
	OutputFormatText OutputFormat = "text"
)

// IsValidOutputFormat checks if the provided format is valid
func IsValidOutputFormat(format string) bool {
	switch OutputFormat(format) {
	case OutputFormatJSON, OutputFormatYAML, OutputFormatText:
		return true
	default:
		return false
	}
}

// WriteOutput writes data to stdout in the specified format
func WriteOutput(data interface{}, format OutputFormat) error {
	switch format {
	case OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	case OutputFormatYAML:
		encoder := yaml.NewEncoder(os.Stdout)
		encoder.SetIndent(2)
		defer encoder.Close()
		return encoder.Encode(data)
	case OutputFormatText:
		// For text format, we'll use the default string representation
		fmt.Println(data)
		return nil
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}

// WriteOutputToFile writes data to a file in the specified format
func WriteOutputToFile(data interface{}, filename string, format OutputFormat) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %v", filename, err)
	}
	defer file.Close()

	switch format {
	case OutputFormatJSON:
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		return encoder.Encode(data)
	case OutputFormatYAML:
		encoder := yaml.NewEncoder(file)
		encoder.SetIndent(2)
		defer encoder.Close()
		return encoder.Encode(data)
	case OutputFormatText:
		_, err := fmt.Fprint(file, data)
		return err
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}
