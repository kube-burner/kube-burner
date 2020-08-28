// Copyright 2020 The Kube-burner Authors.
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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Selector instance
type Selector struct {
	Namespace     string
	LabelSelector string
	FieldSelector string
	ListOptions   metav1.ListOptions
}

// NewSelector creates a new Selector instance
func NewSelector() *Selector {
	return &Selector{
		Namespace: metav1.NamespaceAll,
	}
}

// Configure configures the Selector instance
func (s *Selector) Configure(namespace, labelSelector, fieldSelector string) {
	if namespace != "" {
		s.Namespace = namespace
	}
	s.ListOptions.FieldSelector = fieldSelector
	s.ListOptions.LabelSelector = labelSelector
	s.LabelSelector = labelSelector
	s.FieldSelector = fieldSelector
}
