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
	"bytes"
	"fmt"
	"math"
	"text/template"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/go-commons/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// NewPrometheusClient creates a prometheus struct instance with the given parameters
func NewPrometheusClient(configSpec config.Spec, url, token, username, password, uuid string, tlsVerify bool, step time.Duration, metadata map[string]interface{}) (*Prometheus, error) {
	var err error
	p := Prometheus{
		Step:       step,
		UUID:       uuid,
		ConfigSpec: configSpec,
		Endpoint:   url,
		metadata:   metadata,
	}
	log.Infof("👽 Initializing prometheus client with URL: %s", url)
	p.Client, err = prometheus.NewClient(url, token, username, password, tlsVerify)
	if err != nil {
		return &p, err
	}
	if configSpec.GlobalConfig.MetricsProfile != "" {
		if err := p.readProfile(configSpec.GlobalConfig.MetricsProfile); err != nil {
			log.Fatalf("Metrics-profile error: %v", err.Error())
		}
	}
	return &p, nil
}

// ScrapeJobsMetrics gets all prometheus metrics required and handles them
func (p *Prometheus) ScrapeJobsMetrics(indexer *indexers.Indexer) error {
	start := p.JobList[0].Start
	end := p.JobList[len(p.JobList)-1].End
	elapsed := int(end.Sub(start).Minutes())
	var err error
	var v model.Value
	var renderedQuery bytes.Buffer
	vars := util.EnvToMap()
	vars["elapsed"] = fmt.Sprintf("%dm", elapsed)
	log.Infof("🔍 Scraping prometheus metrics for benchmark from %s to %s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	for _, md := range p.MetricProfile {
		var datapoints []interface{}
		t, _ := template.New("").Parse(md.Query)
		t.Execute(&renderedQuery, vars)
		query := renderedQuery.String()
		renderedQuery.Reset()
		if md.Instant {
			log.Debugf("Instant query: %s", query)
			if v, err = p.Client.Query(query, end); err != nil {
				log.Warnf("Error found with query %s: %s", query, err)
				continue
			}
			if err := p.parseVector(md.MetricName, query, v, &datapoints); err != nil {
				log.Warnf("Error found parsing result from query %s: %s", query, err)
			}
		} else {
			log.Debugf("Range query: %s", query)
			v, err = p.Client.QueryRange(query, start, end, p.Step)
			if err != nil {
				log.Warnf("Error found with query %s: %s", query, err)
				continue
			}
			if err := p.parseMatrix(md.MetricName, query, v, &datapoints); err != nil {
				log.Warnf("Error found parsing result from query %s: %s", query, err)
				continue
			}
		}
		indexerConfig := p.ConfigSpec.GlobalConfig.IndexerConfig
		if indexerConfig.Enabled {
			log.Infof("Indexing metric %s", md.MetricName)
			log.Debugf("Indexing [%d] documents", len(datapoints))
			resp, err := (*indexer).Index(datapoints, indexers.IndexingOpts{MetricName: md.MetricName})
			if err != nil {
				log.Error(err.Error())
			} else {
				log.Info(resp)
			}
		}
	}
	return nil
}

// Find Job fills up job attributes if any
func (p *Prometheus) findJob(timestamp time.Time) config.Job {
	var jobConfig config.Job
	for _, prometheusJob := range p.JobList {
		if timestamp.Before(prometheusJob.End) {
			jobConfig = prometheusJob.JobConfig
		}
	}
	return jobConfig
}

// Parse vector parses results for an instant query
func (p *Prometheus) parseVector(metricName, query string, value model.Value, metrics *[]interface{}) error {
	data, ok := value.(model.Vector)
	if !ok {
		return fmt.Errorf("unsupported result format: %s", value.Type().String())
	}
	for _, vector := range data {
		jobConfig := p.findJob(vector.Timestamp.Time())

		m := p.createMetric(query, metricName, jobConfig, vector.Metric, vector.Value, vector.Timestamp.Time())
		*metrics = append(*metrics, m)
	}
	return nil
}

// Parse matrix parses results for an non-instant query
func (p *Prometheus) parseMatrix(metricName, query string, value model.Value, metrics *[]interface{}) error {
	data, ok := value.(model.Matrix)
	if !ok {
		return fmt.Errorf("unsupported result format: %s", value.Type().String())
	}
	for _, matrix := range data {
		for _, val := range matrix.Values {
			jobConfig := p.findJob(val.Timestamp.Time())

			m := p.createMetric(query, metricName, jobConfig, matrix.Metric, val.Value, val.Timestamp.Time())
			*metrics = append(*metrics, m)
		}
	}
	return nil
}

// ReadProfile reads and parses metric profile configuration
func (p *Prometheus) readProfile(metricsProfile string) error {
	f, err := util.ReadConfig(metricsProfile)
	if err != nil {
		return fmt.Errorf("error reading metrics profile %s: %s", metricsProfile, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&p.MetricProfile); err != nil {
		return fmt.Errorf("error decoding metrics profile %s: %s", metricsProfile, err)
	}
	return nil
}

// Create metric creates metric to be indexed
func (p *Prometheus) createMetric(query, metricName string, jobConfig config.Job, labels model.Metric, value model.SampleValue, timestamp time.Time) metric {
	m := metric{
		Labels:     make(map[string]string),
		UUID:       p.UUID,
		Query:      query,
		MetricName: metricName,
		JobName:    jobConfig.Name,
		JobConfig:  jobConfig,
		Timestamp:  timestamp,
		Metadata:   p.metadata,
	}
	for k, v := range labels {
		if k != "__name__" {
			m.Labels[string(k)] = string(v)
		}
	}
	if math.IsNaN(float64(value)) {
		m.Value = 0
	} else {
		m.Value = float64(value)
	}
	return m
}
