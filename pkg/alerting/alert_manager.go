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
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
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
type alertDefinition struct {
	// PromQL expression to evaluate
	Expr string `yaml:"expr"`
	// Informative comment reported when the alarm is triggered
	Description string `yaml:"description"`
	// Alert Severity
	Severity severityLevel `yaml:"severity"`
	// Evaluate this expresion for the given duration in order fire an alert
	For time.Duration `yaml:"for"`
}

// alert document definition
type alert struct {
	Timestamp   time.Time     `json:"timestamp"`
	UUID        string        `json:"uuid"`
	Severity    severityLevel `json:"severity"`
	Description string        `json:"description"`
	MetricName  string        `json:"metricName"`
}

// AlertManager configuration
type AlertManager struct {
	alertProfile []alertDefinition
	prometheus   *prometheus.Prometheus
	indexer      *indexers.Indexer
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
func NewAlertManager(alertProfileCfg, uuid string, indexer *indexers.Indexer, prometheusClient *prometheus.Prometheus, embedConfig bool) (*AlertManager, error) {
	log.Infof("🔔 Initializing alert manager for prometheus: %v", prometheusClient.Endpoint)
	a := AlertManager{
		prometheus: prometheusClient,
		uuid:       uuid,
		indexer:    indexer,
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
func (a *AlertManager) Evaluate(start, end time.Time) error {
	errs := []error{}
	log.Infof("Evaluating alerts for prometheus: %v", a.prometheus.Endpoint)
	var alertList []interface{}
	elapsed := int(end.Sub(start).Minutes())
	var renderedQuery bytes.Buffer
	vars := util.EnvToMap()
	vars["elapsed"] = fmt.Sprintf("%dm", elapsed)
	for _, alertDef := range a.alertProfile {
		t, _ := template.New("").Parse(alertDef.Expr)
		t.Execute(&renderedQuery, vars)
		expr := renderedQuery.String()
		renderedQuery.Reset()
		log.Debugf("Evaluating expression: '%s'", expr)
		v, err := a.prometheus.Client.QueryRange(expr, start, end, a.prometheus.Step)
		if err != nil {
			log.Warnf("Error performing query %s: %s", expr, err)
			continue
		}
		alertSet, err := a.parseMatrix(v, alertDef)
		if err != nil {
			log.Error(err.Error())
			errs = append(errs, err)
		}
		for _, alert := range alertSet {
			msg := fmt.Sprintf("alert at %v: '%s'", alert.Timestamp.Format(time.RFC3339), alert.Description)
			switch alert.Severity {
			case sevWarn:
				log.Warnf("🚨 %s", msg)
			case sevError:
				errs = append(errs, fmt.Errorf(msg))
				log.Errorf("🚨 %s", msg)
			case sevCritical:
				log.Fatalf("🚨 %s", msg)
			default:
				log.Infof("🚨 %s", msg)
			}
		}
		for _, alertSet := range alertSet {
			alertList = append(alertList, alertSet)
		}
	}
	if len(alertList) > 0 && a.indexer != nil {
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

func (a *AlertManager) parseMatrix(value model.Value, alertDef alertDefinition) ([]alert, error) {
	var renderedDesc bytes.Buffer
	var templateData descriptionTemplate
	var tsList []time.Time
	// The same query can fire multiple alerts, so we have to return an array of them
	var alertSet []alert
	t, _ := template.New("").Parse(strings.Join(append(baseTemplate, alertDef.Description), ""))
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
			tsList = append(tsList, val.Timestamp.Time())
			renderedDesc.Reset()
			// Take 3 decimals
			templateData.Value = math.Round(float64(val.Value)*1000) / 1000
			if err := t.Execute(&renderedDesc, templateData); err != nil {
				return alertSet, fmt.Errorf("alert rendering error: %s", err)
			}
			alertSet = append(alertSet, alert{
				Timestamp:   val.Timestamp.Time().UTC(),
				Severity:    alertDef.Severity,
				Description: renderedDesc.String(),
				MetricName:  alertMetricName,
				UUID:        a.uuid,
			})
		}
	}
	return alertSet, nil
}

func (a *AlertManager) index(alertSet []any) {
	log.Info("Indexing alerts")
	log.Debugf("Indexing [%d] documents", len(alertSet))
	resp, err := (*a.indexer).Index(alertSet, indexers.IndexingOpts{MetricName: alertMetricName})
	if err != nil {
		log.Error(err)
	} else {
		log.Info(resp)
	}
}
