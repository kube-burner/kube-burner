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
	"io"
	"math"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

type severityLevel string

const (
	sevWarn         severityLevel = "warning"
	sevError        severityLevel = "error"
	sevCritical     severityLevel = "critical"
	alertMetricName               = "alert"
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
	Timestamp   time.Time     `json:"timestamp"`
	UUID        string        `json:"uuid"`
	Severity    severityLevel `json:"severity"`
	Description string        `json:"description"`
	MetricName  string        `json:"metricName"`
	ChurnMetric bool          `json:"churnMetric,omitempty"`
}

// AlertManager configuration
type AlertManager struct {
	alertProfile alertProfile
	prometheus   *prometheus.Prometheus
	indexers     []indexers.Indexer
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
func NewAlertManager(alertProfileCfg, uuid string, prometheusClient *prometheus.Prometheus, embedConfig bool, indexers ...indexers.Indexer) (*AlertManager, error) {
	log.Infof("🔔 Initializing alert manager for prometheus: %v", prometheusClient.Endpoint)
	a := AlertManager{
		prometheus: prometheusClient,
		uuid:       uuid,
		indexers:   indexers,
	}
	if err := a.readProfile(alertProfileCfg, embedConfig); err != nil {
		return &a, err
	}
	return &a, nil
}

func (a *AlertManager) readProfile(alertProfileCfg string, embedConfig bool) error {
	var f io.Reader
	var err error
	if embedConfig {
		alertProfileCfg = path.Join(path.Dir(a.prometheus.ConfigSpec.EmbedFSDir), alertProfileCfg)
		f, err = util.ReadEmbedConfig(a.prometheus.ConfigSpec.EmbedFS, alertProfileCfg)
	} else {
		f, err = util.ReadConfig(alertProfileCfg)
	}
	if err != nil {
		log.Fatalf("Error reading alert profile %s: %s", alertProfileCfg, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&a.alertProfile); err != nil {
		return fmt.Errorf("error decoding alert profile %s: %s", alertProfileCfg, err)
	}
	return a.validateTemplates()
}

// Evaluate evaluates expressions
func (a *AlertManager) Evaluate(start, end time.Time, churnStart, churnEnd *time.Time) error {
	errs := []error{}
	log.Infof("Evaluating alerts for prometheus: %v", a.prometheus.Endpoint)
	var alertList []interface{}
	elapsed := int(end.Sub(start).Minutes())
	var renderedQuery bytes.Buffer
	vars := util.EnvToMap()
	vars["elapsed"] = fmt.Sprintf("%dm", elapsed)
	for _, alert := range a.alertProfile {
		t, _ := template.New("").Parse(alert.Expr)
		t.Execute(&renderedQuery, vars)
		expr := renderedQuery.String()
		renderedQuery.Reset()
		log.Debugf("Evaluating expression: '%s'", expr)
		v, err := a.prometheus.Client.QueryRange(expr, start, end, a.prometheus.Step)
		if err != nil {
			log.Warnf("Error performing query %s: %s", expr, err)
			continue
		}
		alertData, err := parseMatrix(v, alert.Description, alert.Severity, churnStart, churnEnd)
		if err != nil {
			log.Error(err.Error())
			errs = append(errs, err)
		}
		for _, alertSet := range alertData {
			alertSet.UUID = a.uuid
			alertList = append(alertList, alertSet)
		}
	}
	if len(alertList) > 0 && len(a.indexers) > 0 {
		a.index(alertList)
	}
	return utilerrors.NewAggregate(errs)
}

func (a *AlertManager) validateTemplates() error {
	for _, a := range a.alertProfile {
		if _, err := template.New("").Parse(strings.Join(append(baseTemplate, a.Description), "")); err != nil {
			return fmt.Errorf("template validation error '%s': %s", a.Description, err)
		}
	}
	return nil
}

func parseMatrix(value model.Value, description string, severity severityLevel, churnStart, churnEnd *time.Time) ([]alert, error) {
	var renderedDesc bytes.Buffer
	var templateData descriptionTemplate
	// The same query can fire multiple alerts, so we have to return an array of them
	var alertSet []alert
	errs := []error{}
	t, _ := template.New("").Parse(strings.Join(append(baseTemplate, description), ""))
	data, ok := value.(model.Matrix)
	if !ok {
		return alertSet, fmt.Errorf("unsupported result format: %s", value.Type().String())
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
				msg := fmt.Errorf("alert rendering error: %s", err)
				log.Error(msg.Error())
				errs = append(errs, err)
			}
			msg := fmt.Sprintf("alert at %v: '%s'", val.Timestamp.Time().UTC().Format(time.RFC3339), renderedDesc.String())
			alert := alert{
				Timestamp:   val.Timestamp.Time().UTC(),
				Severity:    severity,
				Description: renderedDesc.String(),
				MetricName:  alertMetricName,
			}
			if churnStart != nil && alert.Timestamp.After(*churnStart) && alert.Timestamp.Before(*churnEnd) {
				alert.ChurnMetric = true
			}
			alertSet = append(alertSet, alert)
			switch severity {
			case sevWarn:
				log.Warnf("🚨 %s", msg)
			case sevError:
				errs = append(errs, fmt.Errorf(msg))
			case sevCritical:
				log.Fatalf("🚨 %s", msg)
			default:
				log.Infof("🚨 %s", msg)
			}
			break
		}
	}
	return alertSet, utilerrors.NewAggregate(errs)
}

func (a *AlertManager) index(alertSet []interface{}) {
	log.Info("Indexing alerts")
	log.Debugf("Indexing [%d] documents", len(alertSet))
	for _, indexer := range a.indexers {
		resp, err := indexer.Index(alertSet, indexers.IndexingOpts{MetricName: alertMetricName})
		if err != nil {
			log.Error(err)
		} else {
			log.Info(resp)
		}
	}
}
