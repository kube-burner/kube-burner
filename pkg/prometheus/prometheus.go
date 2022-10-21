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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	api "github.com/prometheus/client_golang/api"
	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v3"
)

func (bat authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if bat.username != "" {
		req.SetBasicAuth(bat.username, bat.password)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bat.token))
	return bat.Transport.RoundTrip(req)
}

// NewPrometheusClient creates a prometheus struct instance with the given parameters
func NewPrometheusClient(url, token, username, password, uuid string, tlsVerify bool, pause time.Duration, step time.Duration) (*Prometheus, error) {
	var p Prometheus = Prometheus{
		Pause: pause,
		Step:  step,
		uuid:  uuid,
	}

	log.Info("👽 Initializing prometheus client")
	cfg := api.Config{
		Address: url,
		RoundTripper: authTransport{
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsVerify}},
			token:     token,
			username:  username,
			password:  password,
		},
	}
	c, err := api.NewClient(cfg)
	if err != nil {
		return &p, err
	}
	p.api = apiv1.NewAPI(c)
	// Verify Prometheus connection prior returning
	if err := p.verifyConnection(); err != nil {
		return &p, err
	}
	return &p, nil
}

func (p *Prometheus) verifyConnection() error {
	_, err := p.api.Runtimeinfo(context.TODO())
	if err != nil {
		return err
	}
	log.Debug("Prometheus endpoint verified")
	return nil
}

// ReadProfile reads and parses metric profile configuration
func (p *Prometheus) ReadProfile(metricsProfile string) error {
	f, err := util.ReadConfig(metricsProfile)
	if err != nil {
		log.Fatalf("Error reading metrics profile %s: %s", metricsProfile, err)
	}
	yamlDec := yaml.NewDecoder(f)
	yamlDec.KnownFields(true)
	if err = yamlDec.Decode(&p.MetricProfile); err != nil {
		return fmt.Errorf("Error decoding metrics profile %s: %s", metricsProfile, err)
	}
	return nil
}

// ScrapeMetrics defined in the metrics profile from start to end
func (p *Prometheus) ScrapeMetrics(start, end time.Time, indexer *indexers.Indexer, jobName string) error {
	scraper := []burner.Executor{
		{
			Start: start,
			End:   end,
			Config: config.Job{
				Name: jobName},
		},
	}
	return p.ScrapeJobsMetrics(scraper, indexer)
}

// ScrapeJobsMetrics gets all prometheus metrics required and handles them
func (p *Prometheus) ScrapeJobsMetrics(jobList []burner.Executor, indexer *indexers.Indexer) error {
	start := jobList[0].Start
	end := jobList[len(jobList)-1].End
	elapsed := int(end.Sub(start).Minutes())
	var err error
	var v model.Value
	var renderedQuery bytes.Buffer
	log.Infof("🔍 Scraping prometheus metrics for benchmark from %s to %s", start, end)
	for _, md := range p.MetricProfile {
		var metrics []interface{}
		t, _ := template.New("").Parse(md.Query)
		t.Execute(&renderedQuery, map[string]string{"elapsed": fmt.Sprintf("%dm", elapsed)})
		query := renderedQuery.String()
		renderedQuery.Reset()
		if md.Instant {
			log.Debugf("Instant query: %s", query)
			if v, err = p.Query(query, end); err != nil {
				log.Warnf("Error found with query %s: %s", query, err)
				continue
			}
			if err := p.parseVector(md.MetricName, query, jobList, v, &metrics); err != nil {
				log.Warnf("Error found parsing result from query %s: %s", query, err)
			}
		} else {
			log.Debugf("Range query: %s", query)
			p.QueryRange(query, start, end)
			v, err = p.QueryRange(query, start, end)
			if err != nil {
				log.Warnf("Error found with query %s: %s", query, err)
				continue
			}
			if err := p.parseMatrix(md.MetricName, query, jobList, v, &metrics); err != nil {
				log.Warnf("Error found parsing result from query %s: %s", query, err)
				continue
			}
		}
		if config.ConfigSpec.GlobalConfig.WriteToFile {
			filename := fmt.Sprintf("%s-%s.json", md.MetricName, p.uuid)
			if config.ConfigSpec.GlobalConfig.MetricsDirectory != "" {
				err = os.MkdirAll(config.ConfigSpec.GlobalConfig.MetricsDirectory, 0744)
				if err != nil {
					return fmt.Errorf("Error creating metrics directory: %v: ", err)
				}
				filename = path.Join(config.ConfigSpec.GlobalConfig.MetricsDirectory, filename)
			}
			log.Debugf("Writing to: %s", filename)
			f, err := os.Create(filename)
			if err != nil {
				log.Errorf("Error creating metrics file %s: %s", filename, err)
				continue
			}
			defer f.Close()
			jsonEnc := json.NewEncoder(f)
			err = jsonEnc.Encode(metrics)
			if err != nil {
				log.Errorf("JSON encoding error: %s", err)
			}
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

func (p *Prometheus) parseVector(metricName, query string, jobList []burner.Executor, value model.Value, metrics *[]interface{}) error {
	var jobName string
	data, ok := value.(model.Vector)
	if !ok {
		return fmt.Errorf("Unsupported result format: %s", value.Type().String())
	}
	for _, v := range data {
		for _, job := range jobList {
			if v.Timestamp.Time().Before(job.End) {
				jobName = job.Config.Name
			}
		}
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

func (p *Prometheus) parseMatrix(metricName, query string, jobList []burner.Executor, value model.Value, metrics *[]interface{}) error {
	var jobName string
	data, ok := value.(model.Matrix)
	if !ok {
		return fmt.Errorf("Unsupported result format: %s", value.Type().String())
	}
	for _, v := range data {
		for _, val := range v.Values {
			for _, job := range jobList {
				if val.Timestamp.Time().Before(job.End) {
					jobName = job.Config.Name
				}
			}
			m := metric{
				Labels:     make(map[string]string),
				UUID:       p.uuid,
				Query:      query,
				MetricName: metricName,
				JobName:    jobName,
				Timestamp:  val.Timestamp.Time(),
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
			*metrics = append(*metrics, m)
		}
	}
	return nil
}

// Query prometheus query wrapper
func (p *Prometheus) Query(query string, time time.Time) (model.Value, error) {
	var v model.Value
	v, _, err := p.api.Query(context.TODO(), query, time)
	if err != nil {
		return v, err
	}
	return v, nil
}

// QueryRange prometheus queryRange wrapper
func (p *Prometheus) QueryRange(query string, start, end time.Time) (model.Value, error) {
	var v model.Value
	r := apiv1.Range{Start: start, End: end, Step: p.Step}
	v, _, err := p.api.QueryRange(context.TODO(), query, r)
	if err != nil {
		return v, err
	}
	return v, nil
}
