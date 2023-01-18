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
	"math"
	"strings"
	"text/template"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/prometheus/common/model"

	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"gopkg.in/yaml.v3"
)

type severityLevel string

const (
	Passed                    = 0
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

// alert definition
type alert struct {
	Timestamp   time.Time `json:"timestamp"`
	UUID        string    `json:"uuid"`
	Severity    string    `json:"severity"`
	Description string    `json:"description"`
}

// AlertManager configuration
type AlertManager struct {
	alertProfile alertProfile
	prometheus   *prometheus.Prometheus
	indexer      indexers.Indexer
	indexName    string
	uuid         string
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
func NewAlertManager(alertProfileCfg string, uuid, indexName string, indexer *indexers.Indexer, prometheusClient *prometheus.Prometheus) (*AlertManager, error) {
	log.Info("🔔 Initializing alert manager")
	a := AlertManager{
		prometheus: prometheusClient,
		uuid:       uuid,
		indexer:    *indexer,
		indexName:  indexName,
	}
	if err := a.readProfile(alertProfileCfg); err != nil {
		return &a, err
	}
	return &a, nil
}

func (a *AlertManager) readProfile(alertProfileCfg string) error {
	f, err := util.ReadConfig(alertProfileCfg)
	if err != nil {
		log.Fatalf("Error reading alert profile %s: %s", alertProfileCfg, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&a.alertProfile); err != nil {
		return fmt.Errorf("Error decoding alert profile %s: %s", alertProfileCfg, err)
	}
	return a.validateTemplates()
}

// Evaluate evaluates expressions
func (a *AlertManager) Evaluate(start, end time.Time) int {
	var alertList []interface{}
	elapsed := int(end.Sub(start).Minutes())
	var renderedQuery bytes.Buffer
	result := Passed
	for _, alert := range a.alertProfile {
		t, _ := template.New("").Parse(alert.Expr)
		t.Execute(&renderedQuery, map[string]string{"elapsed": fmt.Sprintf("%dm", elapsed)})
		expr := renderedQuery.String()
		renderedQuery.Reset()
		log.Infof("Evaluating expression: '%s'", expr)
		v, err := a.prometheus.QueryRange(expr, start, end)
		if err != nil {
			log.Warnf("Error performing query %s: %s", expr, err)
			continue
		}
		alarmResult, alertData, err := parseMatrix(v, alert.Description, alert.Severity)
		if err != nil {
			log.Error(err)
		}
		for _, alertSet := range alertData {
			alertSet.UUID = a.uuid
			alertList = append(alertList, alertSet)
		}
		if alarmResult == Failed {
			result = Failed
		}
	}
	if len(alertList) > 0 && a.indexer != nil {
		a.index(alertList)
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

func parseMatrix(value model.Value, description string, severity severityLevel) (int, []alert, error) {
	var renderedDesc bytes.Buffer
	var templateData descriptionTemplate
	// The same query can fire multiple alerts, so we have to return an array of them
	var alertSet []alert
	result := Passed
	t, _ := template.New("").Parse(strings.Join(append(baseTemplate, description), ""))
	data, ok := value.(model.Matrix)
	if !ok {
		return result, alertSet, fmt.Errorf("unsupported result format: %s", value.Type().String())
	}
	for _, v := range data {
		templateData.Labels = make(map[string]string)
		for k, v := range v.Metric {
			templateData.Labels[string(k)] = string(v)
		}
		for _, val := range v.Values {
			renderedDesc.Reset()
			// Take 3 decimals
			templateData.Value = math.Round(float64(val.Value)*1000) / 1000
			if err := t.Execute(&renderedDesc, templateData); err != nil {
				log.Errorf("Rendering error: %s", err)
				result = Failed
			}
			msg := fmt.Sprintf("🚨 Alert at %v: '%s'", val.Timestamp.Time().Format(time.RFC3339), renderedDesc.String())
			alertSet = append(alertSet, alert{
				Timestamp:   val.Timestamp.Time(),
				Severity:    string(severity),
				Description: renderedDesc.String(),
			})
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
	return result, alertSet, nil
}

func (a *AlertManager) index(alertSet []interface{}) error {
	log.Info("Indexing alerts in ", a.indexName)
	a.indexer.Index(a.indexName, alertSet)
	return nil
}
