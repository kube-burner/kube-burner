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

package prometheus

import (
	"github.com/prometheus/common/model"
)

type MetricType struct {
	Type string
}

var Max MetricType = MetricType{"max"}
var Avg MetricType = MetricType{"avg"}

type Metric struct {
	Query      string
	MetricType MetricType
}

var memoryMetrics = []Metric{
	{"foo", Max},
	{"foo", Avg},
}

func getMax(value model.Matrix) model.SampleValue {
	var current model.SampleValue = 0
	for _, v := range value {
		for _, v := range v.Values {
			if v.Value > current {
				current = v.Value
			}
		}
	}
	return current
}

func getAvg(value model.Matrix) model.SampleValue {
	var total model.SampleValue = 0
	for _, v := range value {
		for _, v := range v.Values {
			total += v.Value
		}
	}
	return total / model.SampleValue(len(value))
}
