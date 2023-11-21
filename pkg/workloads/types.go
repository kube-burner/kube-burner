package workloads

import (
	"embed"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"k8s.io/client-go/rest"
)

type ProfileType string
type clusterType string

var MetricsProfileMap = map[string]string{
	"cluster-density-ms":             "metrics-aggregated.yml",
	"cluster-density-v2":             "metrics-aggregated.yml",
	"crd-scale":                      "metrics-aggregated.yml",
	"node-density":                   "metrics.yml",
	"node-density-heavy":             "metrics.yml",
	"node-density-cni":               "metrics.yml",
	"networkpolicy-multitenant":      "metrics.yml",
	"networkpolicy-matchlabels":      "metrics.yml",
	"networkpolicy-matchexpressions": "metrics.yml",
	"pvc-density":                    "metrics.yml",
}

const (
	regular   ProfileType = "regular"
	reporting ProfileType = "reporting"
	both      ProfileType = "both"
	OCP       clusterType = "ocp"
	K8S       clusterType = "k8s"
)

type Config struct {
	UUID            string
	EsServer        string
	EsIndex         string
	QPS             int
	Burst           int
	Gc              bool
	GcMetrics       bool
	Indexer         indexers.IndexerType
	Alerting        bool
	Reporting       bool
	Timeout         time.Duration
	MetricsEndpoint string
	ProfileType     string
	PrometheusURL   string
	PrometheusToken string
}

type BenchmarkMetadata struct {
	ocpmetadata.ClusterMetadata
	UUID            string                 `json:"uuid"`
	Benchmark       string                 `json:"benchmark"`
	Timestamp       time.Time              `json:"timestamp"`
	EndDate         time.Time              `json:"endDate"`
	Passed          bool                   `json:"passed"`
	ExecutionErrors string                 `json:"executionErrors"`
	UserMetadata    map[string]interface{} `json:"metadata,omitempty"`
}

type WorkloadHelper struct {
	Config
	configDir      string
	Metadata       BenchmarkMetadata
	workloadConfig embed.FS
	restConfig     *rest.Config
	OcpMetaAgent   ocpmetadata.Metadata
	shortMetadata  map[string]interface{}
	ClusterType    clusterType
}
