// Copyright 2025 The Kube-burner Authors.
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
	"fmt"
	"io"

	"github.com/kube-burner/kube-burner/pkg/util/fileutils"
	"gopkg.in/yaml.v3"
)

type OverlayReader struct {
	BaseReader       io.Reader
	OverlayPaths     []string
	EmbedCfg         *fileutils.EmbedConfiguration
	processedContent []byte
	readOffset       int
}

type MergeOption struct {
	Path     []string
	MergeKey string
}

var mergeOptions = []MergeOption{
	{
		Path:     []string{"jobs"},
		MergeKey: "name",
	},
	{
		Path:     []string{"jobs", "*", "objects"},
		MergeKey: "objectTemplate",
	},
	{
		Path:     []string{"global", "measurements"},
		MergeKey: "name",
	},
}

// NewOverlayReader creates a new OverlayReader with the given base and overlay paths
func NewOverlayReader(baseLocation string, overlayPaths []string, embedCfg *fileutils.EmbedConfiguration) (*OverlayReader, error) {
	baseReader, err := fileutils.GetWorkloadReader(baseLocation, embedCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read base configuration: %w", err)
	}
	return &OverlayReader{
		BaseReader:   baseReader,
		OverlayPaths: overlayPaths,
		EmbedCfg:     embedCfg,
	}, nil
}

// mergeYAML merges two YAML documents, applying the specified merge options
func mergeYAML(baseYAML, overlayYAML []byte) ([]byte, error) {
	var baseMap map[string]interface{}
	var overlayMap map[string]interface{}
	if err := yaml.Unmarshal(baseYAML, &baseMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal base YAML: %w", err)
	}
	if err := yaml.Unmarshal(overlayYAML, &overlayMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal overlay YAML: %w", err)
	}

	mergedMap := deepMergeMap(baseMap, overlayMap, []string{})
	mergedYAML, err := yaml.Marshal(mergedMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged YAML: %w", err)
	}
	return mergedYAML, nil
}

// deepMergeMap recursively merges two maps, applying merge options
func deepMergeMap(base, overlay map[string]interface{}, currentPath []string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		newPath := append(currentPath, k)
		if baseVal, exists := result[k]; exists {
			if mergeOption := findMergeOption(newPath); mergeOption != nil {
				result[k] = mergeArrayWithKey(baseVal, v, mergeOption.MergeKey, newPath)
			} else if baseMap, ok := baseVal.(map[string]interface{}); ok {
				if overlayMap, ok := v.(map[string]interface{}); ok {
					result[k] = deepMergeMap(baseMap, overlayMap, newPath)
				} else {
					result[k] = v
				}
			} else {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}
	return result
}

// findMergeOption searches for a merge option that matches the current path
func findMergeOption(currentPath []string) *MergeOption {
	for _, option := range mergeOptions {
		if pathMatches(currentPath, option.Path) {
			return &option
		}
	}
	return nil
}

// pathMatches checks if the current path matches the option path
func pathMatches(currentPath, optionPath []string) bool {
	if len(currentPath) != len(optionPath) {
		return false
	}
	for i, segment := range optionPath {
		if segment != "*" && segment != currentPath[i] {
			return false
		}
	}
	return true
}

// mergeArrayWithKey merges arrays, preserving order and merging items with the same key
func mergeArrayWithKey(base, overlay interface{}, mergeKey string, currentPath []string) interface{} {
	baseArray, ok := base.([]interface{})
	if !ok {
		return overlay
	}
	overlayArray, ok := overlay.([]interface{})
	if !ok {
		return overlay
	}
	baseItemsMap := make(map[string]map[string]interface{})
	baseOrder := make([]string, 0)
	for _, item := range baseArray {
		if itemMap, ok := item.(map[string]interface{}); ok {
			keyValue := ""
			if mergeKey != "" {
				if val, exists := itemMap[mergeKey]; exists {
					if str, ok := val.(string); ok {
						keyValue = str
					}
				}
			}
			if keyValue != "" {
				baseItemsMap[keyValue] = itemMap
				baseOrder = append(baseOrder, keyValue)
			}
		}
	}
	for _, overlayItem := range overlayArray {
		if overlayItemMap, ok := overlayItem.(map[string]interface{}); ok {
			keyValue := ""
			if mergeKey != "" {
				if val, exists := overlayItemMap[mergeKey]; exists {
					if str, ok := val.(string); ok {
						keyValue = str
					}
				}
			}
			if keyValue != "" {
				if baseItem, exists := baseItemsMap[keyValue]; exists {
					newPath := append(currentPath, "*")
					mergedItem := deepMergeMap(baseItem, overlayItemMap, newPath)
					baseItemsMap[keyValue] = mergedItem
				} else {
					baseItemsMap[keyValue] = overlayItemMap
					baseOrder = append(baseOrder, keyValue)
				}
			}
		}
	}
	// Convert map back to array, preserving order
	result := make([]interface{}, 0, len(baseItemsMap))
	for _, key := range baseOrder {
		if item, exists := baseItemsMap[key]; exists {
			result = append(result, item)
		}
	}
	return result
}

// Read reads data from the base configuration and applies overlays
func (o *OverlayReader) Read(p []byte) (n int, err error) {
	if o.processedContent != nil {
		n = copy(p, o.processedContent[o.readOffset:])
		o.readOffset += n
		if o.readOffset >= len(o.processedContent) {
			return n, io.EOF
		}
		return n, nil
	}
	baseContent, err := io.ReadAll(o.BaseReader)
	if err != nil {
		return 0, fmt.Errorf("failed to read base configuration: %w", err)
	}
	renderedBaseContent, err := RenderTemplate(baseContent, EnvToMap(), MissingKeyZero, []string{})
	if err != nil {
		return 0, fmt.Errorf("failed to render base configuration with environment variables: %w", err)
	}
	for _, overlayPath := range o.OverlayPaths {
		overlayReader, err := fileutils.GetWorkloadReader(overlayPath, o.EmbedCfg)
		if err != nil {
			return 0, fmt.Errorf("failed to read overlay %s: %w", overlayPath, err)
		}
		overlayContent, err := io.ReadAll(overlayReader)
		if err != nil {
			return 0, fmt.Errorf("failed to read overlay %s: %w", overlayPath, err)
		}
		renderedOverlayContent, err := RenderTemplate(overlayContent, EnvToMap(), MissingKeyZero, []string{})
		if err != nil {
			return 0, fmt.Errorf("failed to render overlay %s with environment variables: %w", overlayPath, err)
		}
		renderedBaseContent, err = mergeYAML(renderedBaseContent, renderedOverlayContent)
		if err != nil {
			return 0, fmt.Errorf("failed to merge overlay %s with base configuration: %w", overlayPath, err)
		}
	}
	o.processedContent = renderedBaseContent
	o.readOffset = 0
	return o.Read(p)
}
