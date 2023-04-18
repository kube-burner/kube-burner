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

	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"github.com/vishnuchalla/go-commons/indexers"
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
	metricsEndpoint string
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
func NewWorkloadHelper(envVars map[string]string, alerting bool, ocpConfig embed.FS, da discovery.Agent, indexing bool, timeout time.Duration, metricsEndpoint string, userMetadata string) WorkloadHelper {
	return WorkloadHelper{
		envVars:         envVars,
		alerting:        alerting,
		metricsEndpoint: metricsEndpoint,
		ocpConfig:       ocpConfig,
		discoveryAgent:  da,
		timeout:         timeout,
		indexing:        indexing,
		userMetadata:    userMetadata,
	}
}

// SetKubeBurnerFlags configures the required environment variables and flags for kube-burner
func (wh *WorkloadHelper) SetKubeBurnerFlags() {
	var err error
	if wh.metricsEndpoint == "" {
		prometheusURL, prometheusToken, err := wh.discoveryAgent.GetPrometheus()
		if err != nil {
			log.Fatal("Error obtaining Prometheus information: ", err.Error())
		}
		wh.prometheusURL = prometheusURL
		wh.prometheusToken = prometheusToken
	}
	wh.envVars["INGRESS_DOMAIN"], err = wh.discoveryAgent.GetDefaultIngressDomain()
	if err != nil {
		log.Fatal("Error obtaining default ingress domain: ", err.Error())
	}
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
	for _, v := range infra.Status.PlatformStatus.Aws.ResourceTags {
		if v.Key == "red-hat-clustertype" {
			wh.Metadata.Platform = v.Value
		}
	}
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

func (wh *WorkloadHelper) run(workload, metricsProfile string) {
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
	var err error
	var indexer *indexers.Indexer
	var alertM *alerting.AlertManager
	var prometheusClients []*prometheus.Prometheus
	var alertMs []*alerting.AlertManager
	var metricsEndpoints []prometheus.MetricEndpoint
	cfg := fmt.Sprintf("%s.yml", workload)
	if _, err := os.Stat(cfg); err != nil {
		log.Debugf("File %v not available in the current directory, extracting it", cfg)
		if err := wh.ExtractWorkload(workload, metricsProfile); err != nil {
			log.Fatalf("Error extracting workload: %v", err)
		}
	} else {
		log.Infof("File %v available in the current directory, using it", cfg)
	}
	configSpec, err := config.Parse(cfg, true)
	if err != nil {
		log.Fatal(err)
	}
	if wh.indexing {
		indexer, err = indexers.NewIndexer(configSpec.GlobalConfig.IndexerConfig)
		if err != nil {
			log.Fatalf("%v indexer: %v", configSpec.GlobalConfig.IndexerConfig.Type, err.Error())
		}
	}
	if wh.metricsEndpoint != "" {
		metrics.DecodeMetricsEndpoint(wh.metricsEndpoint, &metricsEndpoints)
	} else {
		metricsEndpoints = append(metricsEndpoints, prometheus.MetricEndpoint{
			Endpoint:     wh.prometheusURL,
			AlertProfile: alertsProfile,
			Profile:      metricsProfile,
			Token:        wh.prometheusToken,
		})
	}
	for _, metricsEndpoint := range metricsEndpoints {
		// Updating the prometheus endpoint actually being used in spec.
		configSpec.GlobalConfig.PrometheusURL = metricsEndpoint.Endpoint
		configSpec.GlobalConfig.MetricsProfile = metricsEndpoint.Profile
		configSpec.GlobalConfig.AlertProfile = metricsEndpoint.AlertProfile
		p, err := prometheus.NewPrometheusClient(configSpec, metricsEndpoint.Endpoint, metricsEndpoint.Token, "", "", wh.Metadata.UUID, true, 30*time.Second, metadata)
		if err != nil {
			log.Fatal(err)
		}
		if wh.alerting && configSpec.GlobalConfig.AlertProfile != "" {
			alertM, err = alerting.NewAlertManager(configSpec.GlobalConfig.AlertProfile, wh.Metadata.UUID, configSpec.GlobalConfig.IndexerConfig.DefaultIndex, indexer, p)
			if err != nil {
				log.Fatal(err)
			}
		}
		prometheusClients = append(prometheusClients, p)
		alertMs = append(alertMs, alertM)
		alertM = nil
	}
	rc, err = burner.Run(configSpec, wh.Metadata.UUID, prometheusClients, alertMs, indexer, wh.timeout, metadata)
	if err != nil {
		log.Fatal(err)
	}
	wh.Metadata.Passed = rc == 0
	wh.indexMetadata()
	log.Info("👋 Exiting kube-burner ", wh.Metadata.UUID)
	os.Exit(rc)
}

// ExtractWorkload extracts the given workload and metrics profile to the current diretory
func (wh *WorkloadHelper) ExtractWorkload(workload, metricsProfile string) error {
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
	if err = createFile(path.Join(ocpCfgDir, metricsProfile), metricsProfile); err != nil {
		return err
	}
	if err = createFile(path.Join(ocpCfgDir, alertsProfile), alertsProfile); err != nil {
		return err
	}
	return nil
}
