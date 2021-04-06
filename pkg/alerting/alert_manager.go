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
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/prometheus/common/model"

	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"gopkg.in/yaml.v3"
)

type severityLevel string

const (
	// Passed alert
	Passed = 0
	// Failed alert
	Failed                    = 1
	sevWarn     severityLevel = "warning"
	sevError    severityLevel = "error"
	sevCritical severityLevel = "critical"
)

// alertProfile expression list
type alertProfile []struct {
	// PromQL expression to evaluate
	Expr string `yaml:"expr"`
	// Informative comment reported when the alarm is triggered
	Description string `yaml:"description"`
	// Alert Severity
	Severity severityLevel `yaml:"severity"`
}

// AlertManager configuration
type AlertManager struct {
	alertProfile alertProfile
	prometheus   *prometheus.Prometheus
}

var baseTemplate = []string{
	"{{$labels := .Labels}}",
	"{{$value := .Value}}",
}

type descriptionTemplate struct {
	Labels map[string]string
	Value  float64
}

// NewAlertManager creates a new alert manager
func NewAlertManager(alertProfile string, prometheusClient *prometheus.Prometheus) (*AlertManager, error) {
	log.Info("🔔 Initializing alert manager")
	a := AlertManager{
		prometheus: prometheusClient,
	}
	if err := a.readProfile(alertProfile); err != nil {
		return &a, err
	}
	return &a, nil
}

func (a *AlertManager) readProfile(alertProfile string) error {
	f, err := util.ReadConfig(alertProfile)
	if err != nil {
		log.Fatalf("Error reading alert profile %s: %s", alertProfile, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&a.alertProfile); err != nil {
		return fmt.Errorf("Error decoding alert profile %s: %s", alertProfile, err)
	}
	return a.validateTemplates()
}

// Evaluate evaluates expressions
func (a *AlertManager) Evaluate(start, end time.Time) int {
	result := Passed
	for _, alert := range a.alertProfile {
		log.Infof("Evaluating expression: '%s'", alert.Expr)
		v, err := a.prometheus.QueryRange(alert.Expr, start, end)
		if err != nil {
			log.Warnf("Error performing query %s: %s", alert.Expr, err)
			continue
		}
		alarmResult, err := parseMatrix(v, alert.Description, alert.Severity)
		if err != nil {
			log.Error(err)
		}
		if alarmResult == Failed {
			result = Failed
		}
	}
	return result
}

func (a *AlertManager) validateTemplates() error {
	for _, a := range a.alertProfile {
		if _, err := template.New("").Parse(strings.Join(append(baseTemplate, a.Description), "")); err != nil {
			return fmt.Errorf("template validation error '%s': %s", a.Description, err)
		}
	}
	return nil
}

func parseMatrix(value model.Value, description string, severity severityLevel) (int, error) {
	var renderedDesc bytes.Buffer
	var templateData descriptionTemplate
	result := Passed
	t, _ := template.New("").Parse(strings.Join(append(baseTemplate, description), ""))
	data, ok := value.(model.Matrix)
	if !ok {
		return result, fmt.Errorf("Unsupported result format: %s", value.Type().String())
	}

	for _, v := range data {
		// TODO: improve value casting
		templateData.Labels = make(map[string]string)
		for k, v := range v.Metric {
			templateData.Labels[string(k)] = string(v)
		}
		for _, val := range v.Values {
			renderedDesc.Reset()
			templateData.Value = float64(val.Value)
			if err := t.Execute(&renderedDesc, templateData); err != nil {
				log.Errorf("Rendering error: %s", err)
			}
			msg := fmt.Sprintf("Alert triggered at %v: '%s'", val.Timestamp.Time(), renderedDesc.String())
			switch severity {
			case sevWarn:
				log.Warn(msg)
			case sevError:
				result = Failed
				log.Error(msg)
			case sevCritical:
				log.Fatal(msg)
			default:
				log.Info(msg)
			}
			break
		}
	}
	return result, nil
}
