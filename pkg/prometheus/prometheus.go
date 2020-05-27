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
	"net/http"
	"os"
	"path"
	"time"

	api "github.com/prometheus/client_golang/api"
	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/rsevilla87/kube-burner/log"
	"github.com/rsevilla87/kube-burner/pkg/config"
	"github.com/rsevilla87/kube-burner/pkg/indexers"
	"gopkg.in/yaml.v3"
)

type Prometheus struct {
	status         chan string
	api            apiv1.API
	metricsProfile MetricsProfile
	step           time.Duration
	uuid           string
}

// This object implements RoundTripper

type authTransport struct {
	Transport http.RoundTripper
	token     string
	username  string
	password  string
}

type MetricsProfile struct {
	Metrics []string
}

type metric struct {
	Timestamp time.Time
	Value     float64
	Labels    map[string]string
	UUID      string `json:"uuid"`
	JobName   string `json:"jobName"`
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

func NewPrometheusClient(url, token, username, password, metricsProfile, uuid string, tlsVerify bool, prometheusStep time.Duration) (*Prometheus, error) {
	var p Prometheus = Prometheus{
		step: prometheusStep,
		uuid: uuid,
	}
	log.Info("Initializing prometheus client")
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
	if err := p.readProfile(metricsProfile); err != nil {
		return &p, prometheusError(err)
	}
	return &p, nil
}

func (p *Prometheus) verifyConnection() error {
	_, err := p.api.Config(context.TODO())
	if err != nil {
		return err
	}
	log.Debug("Prometheus endpoint verified")
	return nil
}

func (p *Prometheus) readProfile(metricsFile string) error {
	f, err := os.Open(metricsFile)
	if err != nil {
		os.Exit(1)
	}
	yamlDec := yaml.NewDecoder(f)
	if err = yamlDec.Decode(&p.metricsProfile); err != nil {
		return err
	}
	return nil
}

func (p *Prometheus) ScrapeMetrics(start, end time.Time, cfg config.ConfigSpec, jobName string, indexer *indexers.Indexer) error {
	r := apiv1.Range{Start: start, End: end, Step: p.step}
	metrics := []metric{}
	for _, query := range p.metricsProfile.Metrics {
		log.Infof("Quering %s", query)
		v, _, err := p.api.QueryRange(context.TODO(), query, r)
		if err != nil {
			return prometheusError(err)
		}
		if err := p.parseResponse(jobName, v, &metrics); err != nil {
			return err
		}
		if cfg.GlobalConfig.WriteToFile {
			filename := fmt.Sprintf("%s-%s.json", jobName, query)
			if cfg.GlobalConfig.MetricsDirectory != "" {
				err := os.MkdirAll(cfg.GlobalConfig.MetricsDirectory, 0744)
				if err != nil {
					return fmt.Errorf("Error creating metrics directory %s: ", err)
				}
				filename = path.Join(cfg.GlobalConfig.MetricsDirectory, filename)
			}
			log.Debugf("Writing to %s", filename)
			f, err := os.Create(filename)
			if err != nil {
				log.Error("Error creating metrics file %s: %s", filename, err)
			}
			jsonEnc := json.NewEncoder(f)
			jsonEnc.SetIndent("", "  ")
			err = jsonEnc.Encode(metrics)
			if err != nil {
				log.Errorf("JSON encoding error: %s", err)
			}
			f.Close()
		}
	}
	promMetrics := make([]interface{}, len(metrics))
	for _, metric := range metrics {
		promMetrics = append(promMetrics, metric)
	}
	if cfg.GlobalConfig.IndexerConfig.Enabled {
		(*indexer).Index(cfg.GlobalConfig.IndexerConfig.Index, promMetrics)
	}
	return nil
}

func (p *Prometheus) parseResponse(jobName string, value model.Value, metrics *[]metric) error {
	data, ok := value.(model.Matrix)
	if !ok {
		return prometheusError(fmt.Errorf("Unsupported result format: %s", value.Type().String()))
	}
	for _, v := range data {
		for _, val := range v.Values {
			m := metric{
				Labels:  make(map[string]string),
				UUID:    p.uuid,
				JobName: jobName,
			}
			for k, v := range v.Metric {
				m.Labels[string(k)] = string(v)
			}
			m.Value = float64(val.Value)
			m.Timestamp = val.Timestamp.Time()
			*metrics = append(*metrics, m)
		}
	}
	return nil
}
