package metadata

import (
	"time"
)

type Cluster struct {
	MetricName       string    `json:"metricName,omitempty"`
	UUID             string    `json:"uuid"`
	Platform         string    `json:"platform"`
	OCPVersion       string    `json:"ocpVersion"`
	K8SVersion       string    `json:"k8sVersion"`
	MasterNodesType  string    `json:"masterNodesType"`
	WorkerNodesType  string    `json:"workerNodesType"`
	InfraNodesType   string    `json:"infraNodesType"`
	WorkerNodesCount int       `json:"workerNodesCount"`
	InfraNodesCount  int       `json:"infraNodesCount"`
	TotalNodes       int       `json:"totalNodes"`
	SDNType          string    `json:"sdnType"`
	Benchmark        string    `json:"benchmark"`
	Timestamp        time.Time `json:"timestamp"`
	EndDate          time.Time `json:"endDate"`
	ClusterName      string    `json:"clusterName"`
	Passed           bool      `json:"passed"`
}
