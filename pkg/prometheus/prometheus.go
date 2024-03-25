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
	"io"
	"math"
	"path"
	"text/template"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	"github.com/cloud-bulldozer/go-commons/prometheus"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// NewPrometheusClient creates a prometheus struct instance with the given parameters
func NewPrometheusClient(configSpec config.Spec, url string, auth Auth, step time.Duration, metadata map[string]interface{}, embedConfig bool, indexers ...indexers.Indexer) (*Prometheus, error) {
	var err error
	p := Prometheus{
		Step:        step,
		UUID:        configSpec.GlobalConfig.UUID,
		ConfigSpec:  configSpec,
		Endpoint:    url,
		metadata:    metadata,
		embedConfig: embedConfig,
		indexers:    indexers,
	}
	log.Infof("👽 Initializing prometheus client with URL: %s", url)
	p.Client, err = prometheus.NewClient(url, auth.Token, auth.Username, auth.Password, auth.SkipTLSVerify)
	if err != nil {
		return &p, err
	}
	return &p, nil
}

// ScrapeJobsMetrics gets all prometheus metrics required and handles them
func (p *Prometheus) ScrapeJobsMetrics(jobList ...Job) error {
	if len(p.indexers) == 0 {
		log.Debug("Indexing not required for this run")
		return nil
	}
	docsToIndex := make(map[string][]interface{})
	start := jobList[0].Start
	end := jobList[len(jobList)-1].End
	log.Infof("🔍 Scraping %v Profile: %v Start: %v End: %v",
		p.Endpoint,
		p.profileName,
		start.Format(time.RFC3339),
		end.Format(time.RFC3339))
	elapsed := int(end.Sub(start).Seconds())
	var renderedQuery bytes.Buffer
	vars := util.EnvToMap()
	vars["elapsed"] = fmt.Sprintf("%ds", elapsed)
	for _, eachJob := range jobList {
		if eachJob.JobConfig.SkipIndexing {
			log.Infof("Skipping indexing in job: %v", eachJob.JobConfig.Name)
			continue
		}
		jobStart := eachJob.Start
		jobEnd := eachJob.End
		log.Info("Scraping metrics for job: ", eachJob.JobConfig.Name)
		for _, md := range p.MetricProfile {
			requiresInstant := false
			t, _ := template.New("").Parse(md.Query)
			if err := t.Execute(&renderedQuery, vars); err != nil {
				log.Warnf("Error rendering query: %v", err)
				continue
			}
			query := renderedQuery.String()
			renderedQuery.Reset()
			if md.Instant {
				docsToIndex[md.MetricName+"-start"] = append(docsToIndex[md.MetricName+"-start"], p.runInstantQuery(query, md.MetricName+"-start", jobStart, eachJob)...)
				docsToIndex[md.MetricName] = append(docsToIndex[md.MetricName], p.runInstantQuery(query, md.MetricName, jobEnd, eachJob)...)
			} else {
				requiresInstant = ((jobEnd.Sub(jobStart).Milliseconds())%(p.Step.Milliseconds()) != 0)
				docsToIndex[md.MetricName] = append(docsToIndex[md.MetricName], p.runRangeQuery(query, md.MetricName, jobStart, jobEnd, eachJob)...)
			}
			if requiresInstant {
				docsToIndex[md.MetricName] = append(docsToIndex[md.MetricName], p.runInstantQuery(query, md.MetricName, jobEnd, eachJob)...)
			}
		}
	}
	p.indexDatapoints(docsToIndex)
	return nil
}

// Parse vector parses results for an instant query
func (p *Prometheus) parseVector(metricName, query string, job Job, value model.Value, metrics *[]interface{}) error {
	data, ok := value.(model.Vector)
	if !ok {
		return fmt.Errorf("unsupported result format: %s", value.Type().String())
	}
	for _, vector := range data {
		m := p.createMetric(query, metricName, job, vector.Metric, vector.Value, vector.Timestamp.Time().UTC(), true)
		*metrics = append(*metrics, m)
	}
	return nil
}

// Parse matrix parses results for an non-instant query
func (p *Prometheus) parseMatrix(metricName, query string, job Job, value model.Value, metrics *[]interface{}) error {
	data, ok := value.(model.Matrix)
	if !ok {
		return fmt.Errorf("unsupported result format: %s", value.Type().String())
	}
	for _, matrix := range data {
		for _, val := range matrix.Values {
			m := p.createMetric(query, metricName, job, matrix.Metric, val.Value, val.Timestamp.Time().UTC(), false)
			*metrics = append(*metrics, m)
		}
	}
	return nil
}

// ReadProfile reads, parses and validates metric profile configuration
func (p *Prometheus) ReadProfile(metricsProfile string) error {
	var f io.Reader
	var err error
	if p.embedConfig {
		metricsProfile = path.Join(path.Dir(p.ConfigSpec.EmbedFSDir), metricsProfile)
		f, err = util.ReadEmbedConfig(p.ConfigSpec.EmbedFS, metricsProfile)
	} else {
		f, err = util.ReadConfig(metricsProfile)
	}
	p.profileName = metricsProfile
	if err != nil {
		return fmt.Errorf("error reading metrics profile %s: %s", metricsProfile, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&p.MetricProfile); err != nil {
		return fmt.Errorf("error decoding metrics profile %s: %s", metricsProfile, err)
	}
	for i, md := range p.MetricProfile {
		if md.Query == "" {
			return fmt.Errorf("query not defined in %d element", i)
		}
		if md.MetricName == "" {
			return fmt.Errorf("metricName not defined in %d element", i)
		}
	}
	return nil
}

// Create metric creates metric to be indexed
func (p *Prometheus) createMetric(query, metricName string, job Job, labels model.Metric, value model.SampleValue, timestamp time.Time, isInstant bool) metric {
	m := metric{
		Labels:     make(map[string]string),
		UUID:       p.UUID,
		Query:      query,
		MetricName: metricName,
		JobConfig:  job.JobConfig,
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
	if !isInstant && timestamp.After(job.ChurnStart) && timestamp.Before(job.ChurnEnd) {
		m.ChurnMetric = true
	}
	return m
}

// runInstantQuery function to run an instant query
func (p *Prometheus) runInstantQuery(query, metricName string, timestamp time.Time, job Job) []interface{} {
	var v model.Value
	var err error
	var datapoints []interface{}
	log.Debugf("Instant query: %s", query)
	if v, err = p.Client.Query(query, timestamp); err != nil {
		log.Warnf("Error found with query %s: %s", query, err)
		return []interface{}{}
	}
	if err = p.parseVector(metricName, query, job, v, &datapoints); err != nil {
		log.Warnf("Error found parsing result from query %s: %s", query, err)
	}
	return datapoints
}

// runRangeQuery function to run a range query
func (p *Prometheus) runRangeQuery(query, metricName string, jobStart, jobEnd time.Time, job Job) []interface{} {
	var v model.Value
	var err error
	var datapoints []interface{}
	log.Debugf("Range query: %s", query)
	v, err = p.Client.QueryRange(query, jobStart, jobEnd, p.Step)
	if err != nil {
		log.Warnf("Error found with query %s: %s", query, err)
		return []interface{}{}
	}
	if err = p.parseMatrix(metricName, query, job, v, &datapoints); err != nil {
		log.Warnf("Error found parsing result from query %s: %s", query, err)
	}
	return datapoints
}

// Indexes datapoints to a specified indexer.
func (p *Prometheus) indexDatapoints(docsToIndex map[string][]interface{}) {
	for metricName, docs := range docsToIndex {
		log.Infof("Indexing [%d] documents from metric %s", len(docs), metricName)
		for _, indexer := range p.indexers {
			resp, err := indexer.Index(docs, indexers.IndexingOpts{MetricName: metricName})
			if err != nil {
				log.Error(err.Error())
			} else {
				log.Info(resp)
			}
		}
	}
}
