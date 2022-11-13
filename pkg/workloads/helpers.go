package workloads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
)

const (
	metricsProfile     = "metrics.yml"
	alertsProfile      = "alerts.yml"
	metadataMetricName = "clusterMetadata"
)

type WorkloadHelper struct {
	envVars         map[string]string
	prometheusURL   string
	prometheusToken string
	Metadata        clusterMetadata
	alerting        bool
}

type clusterMetadata struct {
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

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(envVars map[string]string, alerting bool) WorkloadHelper {
	return WorkloadHelper{
		envVars:  envVars,
		alerting: alerting,
	}
}

// SetKubeBurnerFlags configures the required environment variables and flags for kube-burner
func (wh *WorkloadHelper) SetKubeBurnerFlags() {
	prometheusURL, prometheusToken, err := discovery.GetPrometheus()
	if err != nil {
		log.Fatal("Error obtaining Prometheus information:", err.Error())
	}
	wh.prometheusURL = prometheusURL
	wh.prometheusToken = prometheusToken
	for k, v := range wh.envVars {
		os.Setenv(k, v)
	}
}

func (wh *WorkloadHelper) GatherMetadata() error {
	infra, err := discovery.GetInfraDetails()
	if err != nil {
		return err
	}
	version, err := discovery.GetVersionInfo()
	if err != nil {
		return err
	}
	nodeInfo, err := discovery.GetNodesInfo()
	if err != nil {
		return err
	}
	sdnType, err := discovery.GetSDNInfo()
	if err != nil {
		return err
	}
	wh.Metadata.MetricName = metadataMetricName
	wh.Metadata.Platform = infra.Status.Platform
	wh.Metadata.ClusterName = infra.Status.InfrastructureName
	wh.Metadata.K8SVersion = version.K8sVersion
	wh.Metadata.OCPVersion = version.OcpVersion
	wh.Metadata.TotalNodes = nodeInfo.TotalNodes
	wh.Metadata.WorkerNodesCount = nodeInfo.WorkerCount
	wh.Metadata.InfraNodesCount = nodeInfo.InfraCount
	wh.Metadata.MasterNodesType = nodeInfo.MasterType
	wh.Metadata.WorkerNodesType = nodeInfo.WorkerType
	wh.Metadata.InfraNodesType = nodeInfo.InfraType
	wh.Metadata.SDNType = sdnType
	wh.Metadata.Timestamp = time.Now().UTC()
	return nil
}

func (wh *WorkloadHelper) IndexMetadata() {
	wh.Metadata.EndDate = time.Now().UTC()
	if wh.envVars["ES_SERVER"] == "" {
		log.Info("No metadata will be indexed")
	}
	esEndpoint := fmt.Sprintf("%v/%v/document", wh.envVars["ES_SERVER"], wh.envVars["ES_INDEX"])
	body, err := json.Marshal(wh.Metadata)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := http.Post(esEndpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Error("Error indexing metadata:", err)
		return
	}
	if resp.StatusCode == http.StatusCreated {
		log.Info("Cluster metadata indexed correctly")
	}
}

func (wh *WorkloadHelper) run(configFile string) {
	var alertM *alerting.AlertManager
	var rc int
	configSpec, err := config.Parse(configFile, true)
	if err != nil {
		log.Fatal(err)
	}
	configSpec.GlobalConfig.MetricsProfile = metricsProfile
	p, err := prometheus.NewPrometheusClient(configSpec, wh.prometheusURL, wh.prometheusToken, "", "", wh.Metadata.UUID, true, 30*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if wh.alerting {
		alertM, err = alerting.NewAlertManager(alertsProfile, p)
		if err != nil {
			log.Fatal(err)
		}
	}
	rc, err = burner.Run(configSpec, wh.Metadata.UUID, p, alertM)
	if err != nil {
		log.Fatal(err)
	}
	wh.Metadata.Passed = rc == 0
	wh.IndexMetadata()
	os.Exit(rc)
}
