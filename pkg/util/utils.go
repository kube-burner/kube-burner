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
	"math"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// RetryWithExponentialBackOff a utility for retrying the given function with exponential backoff.
func RetryWithExponentialBackOff(fn wait.ConditionFunc, duration time.Duration, factor, jitter float64, timeout time.Duration) error {
	steps := int(math.Ceil(math.Log(float64(timeout)/(float64(duration)*(1+jitter))) / math.Log(factor)))
	backoff := wait.Backoff{
		Duration: duration,
		Factor:   factor,
		Jitter:   jitter,
		Steps:    steps,
	}
	return wait.ExponentialBackoff(backoff, fn)
}

func GetBoolValue(m map[string]any, key string) (*bool, error) {
	var ret *bool
	var convertedValue bool

	if value, ok := m[key]; ok {
		switch v := value.(type) {
		case bool:
			ret = &v
		case string:
			switch v {
			case "true":
				convertedValue = true
			case "false":
				convertedValue = false
			default:
				return nil, fmt.Errorf("cannot convert %v to bool", v)
			}
			ret = &convertedValue
		case float64:
			convertedValue = v == 1
			ret = &convertedValue
		default:
			return nil, fmt.Errorf("unexpected type for '%s' field: %T", key, v)
		}
	}
	return ret, nil
}

func GetIntegerValue(m map[string]any, key string) (*int, error) {
	var ret *int
	var intValue int

	if value, ok := m[key]; ok {
		switch v := value.(type) {
		case int:
			ret = &v
		case float64:
			intValue = int(v)
			ret = &intValue
		case string:
			if _, err := fmt.Sscanf(v, "%d", &intValue); err == nil {
				ret = &intValue
			} else {
				return nil, fmt.Errorf("cannot convert %v to int", v)
			}
		default:
			return nil, fmt.Errorf("unexpected type for 'paused' field: %T", v)
		}
	}
	return ret, nil
}

func GetStringValue(m map[string]any, key string) (*string, error) {
	var ret *string
	var strValue string

	if value, ok := m[key]; ok {
		switch v := value.(type) {
		case string:
			ret = &v
		case float64:
			// Convert float64 to string, e.g., 123.0 -> "123"
			strValue = fmt.Sprintf("%v", v)
			ret = &strValue
		case bool:
			// Convert bool to string, e.g., true -> "true"
			if v {
				strValue = "true"
			} else {
				strValue = "false"
			}
			ret = &strValue
		default:
			return nil, fmt.Errorf("unexpected type for '%s' field: %T", key, v)
		}
	}
	return ret, nil
}

// ResourceToGVR maps resource kind to appropriate GVR
func ResourceToGVR(config *rest.Config, kind, apiVersion string) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("invalid apiVersion %s: %w", apiVersion, err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to create discovery client: %w", err)
	}

	apiGroupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get API group resources: %w", err)
	}

	mapper := restmapper.NewDiscoveryRESTMapper(apiGroupResources)

	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    kind,
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gv.Version)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get REST mapping: %w", err)
	}

	return mapping.Resource, nil
}

// ConvertAnyToTyped converts an unstructured object to a typed object (e.g., *corev1.Pod)
func ConvertAnyToTyped[T any](obj any) (*T, error) {
	unstrObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("expected *unstructured.Unstructured, got %T", obj)
	}
	data, err := json.Marshal(unstrObj.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal unstructured object: %w", err)
	}
	var typedObj T
	if err := json.Unmarshal(data, &typedObj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into typed object: %w", err)
	}
	return &typedObj, nil
}

// NormalizeLabels replaces dots in label keys with underscores to avoid issues when exporting metrics to certain backends.
func NormalizeLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[strings.ReplaceAll(k, ".", "_")] = v
	}
	return out
}
