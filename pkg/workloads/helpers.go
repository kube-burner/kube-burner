// Copyright 2022 The Kube-burner Authors.
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

package workloads

import (
	"bytes"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/cloud-bulldozer/kube-burner/pkg/indexers"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
)

const (
	alertsProfile      = "alerts.yml"
	metadataMetricName = "clusterMetadata"
	ocpCfgDir          = "ocp-config"
)

type WorkloadHelper struct {
	envVars         map[string]string
	prometheusURL   string
	prometheusToken string
	timeout         time.Duration
	Metadata        clusterMetadata
	alerting        bool
	ocpConfig       embed.FS
	discoveryAgent  discovery.Agent
	indexing        bool
	userMetadata    string
}

type clusterMetadata struct {
	MetricName       string                 `json:"metricName,omitempty"`
	UUID             string                 `json:"uuid"`
	Platform         string                 `json:"platform"`
	OCPVersion       string                 `json:"ocpVersion"`
	K8SVersion       string                 `json:"k8sVersion"`
	MasterNodesType  string                 `json:"masterNodesType"`
	WorkerNodesType  string                 `json:"workerNodesType"`
	InfraNodesType   string                 `json:"infraNodesType"`
	WorkerNodesCount int                    `json:"workerNodesCount"`
	InfraNodesCount  int                    `json:"infraNodesCount"`
	TotalNodes       int                    `json:"totalNodes"`
	SDNType          string                 `json:"sdnType"`
	Benchmark        string                 `json:"benchmark"`
	Timestamp        time.Time              `json:"timestamp"`
	EndDate          time.Time              `json:"endDate"`
	ClusterName      string                 `json:"clusterName"`
	Passed           bool                   `json:"passed"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(envVars map[string]string, alerting bool, ocpConfig embed.FS, da discovery.Agent, indexing bool, timeout time.Duration, userMetadata string) WorkloadHelper {
	return WorkloadHelper{
		envVars:        envVars,
		alerting:       alerting,
		ocpConfig:      ocpConfig,
		discoveryAgent: da,
		timeout:        timeout,
		indexing:       indexing,
		userMetadata:   userMetadata,
	}
}

// SetKubeBurnerFlags configures the required environment variables and flags for kube-burner
func (wh *WorkloadHelper) SetKubeBurnerFlags() {
	prometheusURL, prometheusToken, err := wh.discoveryAgent.GetPrometheus()
	if err != nil {
		log.Fatal("Error obtaining Prometheus information: ", err.Error())
	}
	wh.envVars["INGRESS_DOMAIN"], err = wh.discoveryAgent.GetDefaultIngressDomain()
	if err != nil {
		log.Fatal("Error obtaining default ingress domain: ", err.Error())
	}
	wh.prometheusURL = prometheusURL
	wh.prometheusToken = prometheusToken
	for k, v := range wh.envVars {
		os.Setenv(k, v)
	}
}

func (wh *WorkloadHelper) GatherMetadata() error {
	infra, err := wh.discoveryAgent.GetInfraDetails()
	if err != nil {
		return err
	}
	version, err := wh.discoveryAgent.GetVersionInfo()
	if err != nil {
		return err
	}
	nodeInfo, err := wh.discoveryAgent.GetNodesInfo()
	if err != nil {
		return err
	}
	sdnType, err := wh.discoveryAgent.GetSDNInfo()
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

func (wh *WorkloadHelper) indexMetadata() {
	wh.Metadata.EndDate = time.Now().UTC()
	if wh.envVars["ES_SERVER"] == "" {
		log.Info("No metadata will be indexed")
		return
	}
	esEndpoint := fmt.Sprintf("%v/%v/_doc", wh.envVars["ES_SERVER"], wh.envVars["ES_INDEX"])
	body, _ := json.Marshal(wh.Metadata)
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Post(esEndpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Error("Error indexing metadata: ", err)
		return
	}
	if resp.StatusCode == http.StatusCreated {
		log.Info("Cluster metadata indexed correctly")
	} else {
		b, _ := io.ReadAll(resp.Body)
		log.Errorf("Error indexing metadata, code: %v body: %s", resp.StatusCode, b)
	}
}

func (wh *WorkloadHelper) run(workload, metrics string) {
	metadata := map[string]interface{}{
		"platform":   wh.Metadata.Platform,
		"ocpVersion": wh.Metadata.OCPVersion,
		"k8sVersion": wh.Metadata.K8SVersion,
		"totalNodes": wh.Metadata.TotalNodes,
		"sdnType":    wh.Metadata.SDNType,
	}
	if wh.userMetadata != "" {
		userMetadataContent, err := util.ReadUserMetadata(wh.userMetadata)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
		// Combine provided userMetadata with the regular OCP metadata
		for k, v := range userMetadataContent {
			metadata[k] = v
		}
		wh.Metadata.Metadata = userMetadataContent
	}
	var rc int
	var indexer *indexers.Indexer
	var alertM *alerting.AlertManager
	cfg := fmt.Sprintf("%s.yml", workload)
	if _, err := os.Stat(cfg); err != nil {
		log.Debug("Workload not available in the current directory, extracting it")
		if err := wh.ExtractWorkload(workload, metrics); err != nil {
			log.Fatalf("Error extracting workload: %v", err)
		}
	}
	configSpec, err := config.Parse(cfg, true)
	if err != nil {
		log.Fatal(err)
	}
	if wh.indexing {
		indexer, err = indexers.NewIndexer(configSpec)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
	configSpec.GlobalConfig.MetricsProfile = metrics
	p, err := prometheus.NewPrometheusClient(configSpec, wh.prometheusURL, wh.prometheusToken, "", "", wh.Metadata.UUID, true, 30*time.Second, metadata)
	if err != nil {
		log.Fatal(err)
	}
	if wh.alerting {
		alertM, err = alerting.NewAlertManager(alertsProfile, wh.Metadata.UUID, configSpec.GlobalConfig.IndexerConfig.DefaultIndex, indexer, p)
		if err != nil {
			log.Fatal(err)
		}
	}
	rc, err = burner.Run(configSpec, wh.Metadata.UUID, p, alertM, indexer, wh.timeout, metadata)
	if err != nil {
		log.Fatal(err)
	}
	wh.Metadata.Passed = rc == 0
	wh.indexMetadata()
	log.Info("👋 Exiting kube-burner ", wh.Metadata.UUID)
	os.Exit(rc)
}

// ExtractWorkload extracts the given workload and metrics profile to the current diretory
func (wh *WorkloadHelper) ExtractWorkload(workload, metrics string) error {
	dirContent, err := wh.ocpConfig.ReadDir(path.Join(ocpCfgDir, workload))
	if err != nil {
		return err
	}
	createFile := func(filePath, fileName string) error {
		fileContent, _ := wh.ocpConfig.ReadFile(filePath)
		fd, err := os.Create(fileName)
		if err != nil {
			return err
		}
		defer fd.Close()
		fd.Write(fileContent)
		return nil
	}
	for _, f := range dirContent {
		err := createFile(path.Join(ocpCfgDir, workload, f.Name()), f.Name())
		if err != nil {
			return err
		}
	}
	if err = createFile(path.Join(ocpCfgDir, metrics), metrics); err != nil {
		return err
	}
	if err = createFile(path.Join(ocpCfgDir, alertsProfile), alertsProfile); err != nil {
		return err
	}
	return nil
}
