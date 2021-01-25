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

package alerting

import (
	"testing"

	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
)

func TestNewAlertManager(t *testing.T) {
	alertProfile := "../../examples/alerts.yml"
	prometheusClient := &prometheus.Prometheus{}
	_, err := NewAlertManager(alertProfile, prometheusClient)
	if err != nil {
		t.Error(err)
	}
}

func TestReadProfile(t *testing.T) {
	a := AlertManager{
		prometheus: &prometheus.Prometheus{},
	}
	cases := []struct {
		alertProfile string
		failed       bool
	}{
		{
			alertProfile: "../../examples/alerts.yml",
			failed:       false,
		},
		{
			alertProfile: "https://raw.githubusercontent.com/cloud-bulldozer/kube-burner/master/test/alert-profile.yaml",
			failed:       false,
		},
		{
			alertProfile: "https://raw.githubusercontent.com/cloud-bulldozer/kube-burner/master/.goreleaser.yml",
			failed:       true,
		},
		{
			alertProfile: "foo",
			failed:       true,
		},
		{
			alertProfile: "https://unknown.com/alert-profile.yaml",
			failed:       true,
		},
	}
	for _, testCase := range cases {
		err := a.readProfile(testCase.alertProfile)
		returnErr := err != nil
		if returnErr != testCase.failed {
			t.Errorf("Alert profile test case failed: %s", testCase.alertProfile)
			t.Error(err)
		}
	}
}
