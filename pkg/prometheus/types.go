package prometheus

import (
	"net/http"
	"time"

	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// Prometheus describes the prometheus connection
type Prometheus struct {
	api           apiv1.API
	MetricProfile metricProfile
	Step          time.Duration
	uuid          string
}

// This object implements RoundTripper
type authTransport struct {
	Transport http.RoundTripper
	token     string
	username  string
	password  string
}

// metricProfile describes what metrics kube-burner collects
type metricProfile []struct {
	Query      string `yaml:"query"`
	MetricName string `yaml:"metricName"`
	IndexName  string `yaml:"indexName"`
	Instant    bool   `yaml:"instant"`
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
