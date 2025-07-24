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

package overlay

import (
	"fmt"
	"maps"

	"gopkg.in/yaml.v3"
)

type MergeOption struct {
	Path     []string
	MergeKey string
}

// mergeOptions defines how arrays should be merged at specific paths in the YAML structure.
// Instead of replacing entire arrays, these options enable intelligent merging of array elements
// by using a specified field as a unique identifier (MergeKey).
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

// mergeYAML merges two YAML documents, applying the specified merge options
func MergeYAML(baseYAML, overlayYAML []byte) ([]byte, error) {
	var baseMap map[string]any
	var overlayMap map[string]any
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
func deepMergeMap(base, overlay map[string]any, currentPath []string) map[string]any {
	result := make(map[string]any)
	maps.Copy(result, base)
	for k, v := range overlay {
		newPath := append(currentPath, k)
		baseVal, exists := result[k]
		if !exists {
			result[k] = v
			continue
		}
		if mergeOption := findMergeOption(newPath); mergeOption != nil {
			result[k] = mergeArrayWithKey(baseVal, v, mergeOption.MergeKey, newPath)
			continue
		}
		baseMap, baseIsMap := baseVal.(map[string]any)
		overlayMap, overlayIsMap := v.(map[string]any)
		if baseIsMap && overlayIsMap {
			result[k] = deepMergeMap(baseMap, overlayMap, newPath)
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
func mergeArrayWithKey(base, overlay any, mergeKey string, currentPath []string) any {
	baseArray, baseIsArray := base.([]any)
	overlayArray, overlayIsArray := overlay.([]any)
	if !baseIsArray || !overlayIsArray || mergeKey == "" {
		return overlay
	}
	baseItemsMap := make(map[string]map[string]any)
	baseOrder := make([]string, 0)
	for _, item := range baseArray {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		val, exists := itemMap[mergeKey]
		if !exists {
			continue
		}
		str, ok := val.(string)
		if !ok {
			continue
		}
		baseItemsMap[str] = itemMap
		baseOrder = append(baseOrder, str)
	}
	for _, overlayItem := range overlayArray {
		overlayItemMap, ok := overlayItem.(map[string]any)
		if !ok {
			continue
		}
		val, exists := overlayItemMap[mergeKey]
		if !exists {
			continue
		}
		str, ok := val.(string)
		if !ok {
			continue
		}
		if baseItem, exists := baseItemsMap[str]; exists {
			newPath := append(currentPath, "*")
			mergedItem := deepMergeMap(baseItem, overlayItemMap, newPath)
			baseItemsMap[str] = mergedItem
		} else {
			baseItemsMap[str] = overlayItemMap
			baseOrder = append(baseOrder, str)
		}
	}
	result := make([]any, 0, len(baseItemsMap))
	for _, key := range baseOrder {
		if item, exists := baseItemsMap[key]; exists {
			result = append(result, item)
		}
	}
	return result
}
