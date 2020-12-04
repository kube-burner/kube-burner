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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	api "github.com/prometheus/client_golang/api"
	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v3"
)

// Prometheus describes the prometheus connection
type Prometheus struct {
	api            apiv1.API
	MetricsProfile MetricsProfile
	Step           time.Duration
	uuid           string
}

// This object implements RoundTripper

type authTransport struct {
	Transport http.RoundTripper
	token     string
	username  string
	password  string
}

type metricDefinition struct {
	Query      string `yaml:"query"`
	MetricName string `yaml:"metricName"`
	IndexName  string `yaml:"indexName"`
	Instant    bool   `yaml:"instant"`
}

// MetricsProfile describes what metrics kube-burner collects
type MetricsProfile struct {
	Metrics []metricDefinition `yaml:"metrics"`
}

type metric struct {
	Timestamp  time.Time         `json:"timestamp"`
	Labels     map[string]string `json:"labels"`
	Value      float64           `json:"value"`
	UUID       string            `json:"uuid"`
	Query      string            `json:"query"`
	MetricName string            `json:"metricName,omitempty"`
	JobName    string            `json:"jobName,omitempty"`
}

func (bat authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if bat.username != "" {
		req.SetBasicAuth(bat.username, bat.password)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bat.token))
	return bat.Transport.RoundTrip(req)
}

func prometheusError(err error) error {
	return fmt.Errorf("Prometheus error: %s", err.Error())
}

// NewPrometheusClient creates a prometheus struct instance with the given parameters
func NewPrometheusClient(url, token, username, password, uuid string, tlsVerify bool, prometheusStep time.Duration) (*Prometheus, error) {
	var p Prometheus = Prometheus{
		Step: prometheusStep,
		uuid: uuid,
	}
	log.Info("👽 Initializing prometheus client")
	cfg := api.Config{
		Address: url,
		RoundTripper: authTransport{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsVerify}},
			token:     token,
			username:  username,
			password:  password,
		},
	}
	c, err := api.NewClient(cfg)
	if err != nil {
		return &p, prometheusError(err)
	}
	p.api = apiv1.NewAPI(c)
	if err := p.verifyConnection(); err != nil {
		return &p, prometheusError(err)
	}
	return &p, nil
}

func (p *Prometheus) verifyConnection() error {
	_, err := p.api.Config(context.TODO())
	if err != nil {
		return prometheusError(err)
	}
	log.Debug("Prometheus endpoint verified")
	return nil
}

// ReadProfile reads and parses metric profile configuration
func (p *Prometheus) ReadProfile(metricsProfile string) error {
	var f io.Reader
	f, err := os.Open(metricsProfile)
	// If the metricsProfile file does not exist we try to read it from an URL
	if os.IsNotExist(err) {
		f, err = util.ReadURL(metricsProfile)
	}
	if err != nil {
		log.Fatalf("Error reading metrics profile %s: %s", metricsProfile, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&p.MetricsProfile); err != nil {
		return fmt.Errorf("Error decoding metrics profile %s: %s", metricsProfile, err)
	}
	return nil
}

// ScrapeMetrics gets all prometheus metrics required and handles them
func (p *Prometheus) ScrapeMetrics(start, end time.Time, indexer *indexers.Indexer) error {
	var filename, jobName string
	var err error
	var v model.Value
	log.Infof("🔍 Scraping prometheus metrics from %s to %s", start, end)
	for _, md := range p.MetricsProfile.Metrics {
		var metrics []interface{}
		// IndexCMD can work w/o specifying any job
		if len(config.ConfigSpec.Jobs) > 0 {
			jobName = config.ConfigSpec.Jobs[0].Name
		}
		if md.Instant {
			log.Infof("Instant query: %s", md.Query)
			if v, err = p.Query(md.Query, time.Now().UTC()); err != nil {
				return err
			}
			if err := p.parseVector(md.MetricName, md.Query, jobName, v, &metrics); err != nil {
				return err
			}
		} else {
			log.Infof("Range query: %s", md.Query)
			p.QueryRange(md.Query, start, end)
			v, err = p.QueryRange(md.Query, start, end)
			if err != nil {
				return err
			}
			if err := p.parseMatrix(md.MetricName, md.Query, jobName, v, &metrics); err != nil {
				return err
			}
		}
		if config.ConfigSpec.GlobalConfig.WriteToFile {
			filename = fmt.Sprintf("%s.json", md.MetricName)
			if config.ConfigSpec.GlobalConfig.MetricsDirectory != "" {
				err = os.MkdirAll(config.ConfigSpec.GlobalConfig.MetricsDirectory, 0744)
				if err != nil {
					return fmt.Errorf("Error creating metrics directory %s: ", err)
				}
				filename = path.Join(config.ConfigSpec.GlobalConfig.MetricsDirectory, filename)
			}
			log.Infof("Writing to: %s", filename)
			f, err := os.Create(filename)
			if err != nil {
				log.Errorf("Error creating metrics file %s: %s", filename, err)
			}
			jsonEnc := json.NewEncoder(f)
			jsonEnc.SetIndent("", "    ")
			err = jsonEnc.Encode(metrics)
			if err != nil {
				log.Errorf("JSON encoding error: %s", err)
			}
			f.Close()
		}
		if config.ConfigSpec.GlobalConfig.IndexerConfig.Enabled {
			indexName := config.ConfigSpec.GlobalConfig.IndexerConfig.DefaultIndex
			if md.IndexName != "" {
				indexName = strings.ToLower(md.IndexName)
			}
			(*indexer).Index(indexName, metrics)
		}
	}
	return nil
}

func (p *Prometheus) parseVector(metricName, query, jobName string, value model.Value, metrics *[]interface{}) error {
	data, ok := value.(model.Vector)
	if !ok {
		return prometheusError(fmt.Errorf("Unsupported result format: %s", value.Type().String()))
	}
	for _, v := range data {
		m := metric{
			Labels:     make(map[string]string),
			UUID:       p.uuid,
			Query:      query,
			MetricName: metricName,
			JobName:    jobName,
		}
		for k, v := range v.Metric {
			if k == "__name__" {
				continue
			}
			m.Labels[string(k)] = string(v)
		}
		if math.IsNaN(float64(v.Value)) {
			m.Value = 0
		} else {
			m.Value = float64(v.Value)
		}
		m.Timestamp = v.Timestamp.Time()
		*metrics = append(*metrics, m)
	}
	return nil
}

func (p *Prometheus) parseMatrix(metricName, query, jobName string, value model.Value, metrics *[]interface{}) error {
	data, ok := value.(model.Matrix)
	if !ok {
		return prometheusError(fmt.Errorf("Unsupported result format: %s", value.Type().String()))
	}
	for _, v := range data {
		for _, val := range v.Values {
			m := metric{
				Labels:     make(map[string]string),
				UUID:       p.uuid,
				Query:      query,
				MetricName: metricName,
				JobName:    jobName,
			}
			for k, v := range v.Metric {
				if k == "__name__" {
					continue
				}
				m.Labels[string(k)] = string(v)
			}
			if math.IsNaN(float64(val.Value)) {
				m.Value = 0
			} else {
				m.Value = float64(val.Value)
			}
			m.Timestamp = val.Timestamp.Time()
			*metrics = append(*metrics, m)
		}
	}
	return nil
}

// Query prometheues query wrapper
func (p *Prometheus) Query(query string, time time.Time) (model.Value, error) {
	var v model.Value
	v, _, err := p.api.Query(context.TODO(), query, time)
	if err != nil {
		return v, prometheusError(err)
	}
	return v, nil
}

// QueryRange prometheues QueryRange wrapper
func (p *Prometheus) QueryRange(query string, start, end time.Time) (model.Value, error) {
	var v model.Value
	r := apiv1.Range{Start: start, End: end, Step: p.Step}
	v, _, err := p.api.QueryRange(context.TODO(), query, r)
	if err != nil {
		return v, prometheusError(err)
	}
	return v, nil
}
