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
	"fmt"
	"math"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

func GetBoolValue(m map[string]any, key string) *bool {
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
				log.Fatalf("cannot convert %v to bool", v)
			}
			ret = &convertedValue
		case float64:
			convertedValue = v == 1
			ret = &convertedValue
		default:
			log.Fatalf("unexpected type for '%s' field: %T", key, v)
		}
	}
	return ret
}

func GetIntegerValue(m map[string]any, key string) *int {
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
				log.Fatalf("cannot convert %v to int", v)
			}
		default:
			log.Fatalf("unexpected type for 'paused' field: %T", v)
		}
	}
	return ret
}

func GetStringValue(m map[string]any, key string) *string {
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
			log.Fatalf("unexpected type for '%s' field: %T", key, v)
		}
	}
	return ret
}

// ResourceToRESTClient maps resource kind to appropriate REST client
func ResourceToRESTClient(clientset kubernetes.Interface, kind string) (*rest.RESTClient, error) {
	kind = strings.ToLower(kind)
	switch kind {
	// CoreV1 resources
	case "pod",
		"service",
		"endpoint",
		"configmap",
		"secret",
		"persistentvolume",
		"persistentvolumeclaim",
		"node",
		"namespace",
		"event",
		"serviceaccount",
		"limitrange",
		"resourcequota":
		return clientset.CoreV1().RESTClient().(*rest.RESTClient), nil

	// AppsV1 resources
	case "deployment",
		"statefulset",
		"daemonset",
		"replicaset":
		return clientset.AppsV1().RESTClient().(*rest.RESTClient), nil

	// BatchV1 resources
	case "job",
		"cronjob":
		return clientset.BatchV1().RESTClient().(*rest.RESTClient), nil

	// NetworkingV1 resources
	case "ingress",
		"networkpolicy":
		return clientset.NetworkingV1().RESTClient().(*rest.RESTClient), nil

	// RBACV1 resources
	case "role",
		"clusterrole",
		"rolebinding",
		"clusterrolebinding":
		return clientset.RbacV1().RESTClient().(*rest.RESTClient), nil

	default:
		return nil, fmt.Errorf("unsupported resource kind: %s", kind)
	}
}

// NaivePlural gives naive plurals
func NaivePlural(kind string) string {
	kindLower := strings.ToLower(kind)
	if strings.HasSuffix(kindLower, "ss") {
		return kindLower + "es"
	}
	if strings.HasSuffix(kindLower, "cy") {
		return strings.TrimSuffix(kindLower, "cy") + "cies"
	}
	return kindLower + "s"
}
