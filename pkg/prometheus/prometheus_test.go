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
	"testing"
	"time"

	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type promTestClient struct {
	apiv1.API
}

var prometheus Prometheus

/*
func (at *promTestClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, apiv1.Warnings, error) {
	fmt.Println("Querying", query)
}
*/

func init() {
	prometheus = Prometheus{
		api: &promTestClient{},
	}
}

func TestParseVector(t *testing.T) {
	ts := time.Now()
	type inputStruct struct {
		metricName string
		query      string
		jobName    string
		failed     bool
		value      model.Value
		result     []interface{}
	}
	baseTest := inputStruct{
		metricName: "foo",
		query:      "query_name",
		jobName:    "myJob",
		failed:     false,
		value: model.Vector{
			&model.Sample{
				Value:     2,
				Timestamp: model.Time(ts.Unix()),
				Metric:    model.Metric(map[model.LabelName]model.LabelValue{"foo": "bar"}),
			},
		},
		result: []interface{}{
			metric{Timestamp: ts},
		},
	}

	testCases := []inputStruct{
		baseTest,
		func() inputStruct {
			baseTest.value = model.Matrix{}
			baseTest.failed = true
			return baseTest
		}(),
	}
	for _, testCase := range testCases {
		var result []interface{}
		err := prometheus.parseVector(testCase.metricName, testCase.query, testCase.jobName, testCase.value, &result)
		returnErr := err != nil
		t.Logf("Comparing %v with %v", returnErr, testCase.failed)
		if returnErr {
			if returnErr != testCase.failed {
				t.Fatal(err)
			}
		} else {
			data, ok := result[0].(metric)
			if !ok {
				t.Errorf("Return data is not of type metric")
			}
			if data.JobName != testCase.jobName {
				t.Errorf("%s != %s", data.JobName, testCase.jobName)
			}
		}
	}
}
