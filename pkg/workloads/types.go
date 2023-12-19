package workloads

import (
	"embed"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"k8s.io/client-go/rest"
)

type clusterType string

const (
	OCP       clusterType = "ocp"
	K8S       clusterType = "k8s"
	configDir             = "config"
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
	Indexing        bool
	Timeout         time.Duration
	MetricsEndpoint string
	UserMetadata    string
	ConfigDir       string
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
	prometheusURL   string
	prometheusToken string
	Metadata        BenchmarkMetadata
	embedConfig     embed.FS
	MetadataAgent   ocpmetadata.Metadata
	RestConfig      *rest.Config
	metricsMetadata map[string]interface{}
	clusterType     clusterType
}
