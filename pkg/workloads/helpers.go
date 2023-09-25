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
	"embed"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"github.com/cloud-bulldozer/kube-burner/pkg/alerting"
	"github.com/cloud-bulldozer/kube-burner/pkg/burner"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	"github.com/cloud-bulldozer/kube-burner/pkg/prometheus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"github.com/cloud-bulldozer/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	alertsProfile         = "alerts.yml"
	ocpCfgDir             = "ocp-config"
	stepSize              = 30 * time.Second
	clusterMetadataMetric = "clusterMetadata"
	reportProfile         = "metrics-report.yml"
)

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
	envVars         map[string]string
	prometheusURL   string
	prometheusToken string
	metricsEndpoint string
	timeout         time.Duration
	Metadata        BenchmarkMetadata
	alerting        bool
	ocpConfig       embed.FS
	ocpMetaAgent    ocpmetadata.Metadata
	reporting       bool
	restConfig      *rest.Config
}

var configSpec config.Spec

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(envVars map[string]string, alerting, reporting bool, ocpConfig embed.FS, timeout time.Duration, metricsEndpoint string) WorkloadHelper {
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); kubeconfig == "" && !os.IsNotExist(err) {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	ocpMetadata, err := ocpmetadata.NewMetadata(restConfig)
	if err != nil {
		log.Fatal(err.Error())
	}
	return WorkloadHelper{
		envVars:         envVars,
		alerting:        alerting,
		reporting:       reporting,
		metricsEndpoint: metricsEndpoint,
		ocpConfig:       ocpConfig,
		ocpMetaAgent:    ocpMetadata,
		timeout:         timeout,
		restConfig:      restConfig,
	}
}

var indexer *indexers.Indexer

// SetKubeBurnerFlags configures the required environment variables and flags for kube-burner
func (wh *WorkloadHelper) SetKubeBurnerFlags() {
	var err error
	if wh.metricsEndpoint == "" {
		prometheusURL, prometheusToken, err := wh.ocpMetaAgent.GetPrometheus()
		if err != nil {
			log.Fatal("Error obtaining Prometheus information: ", err.Error())
		}
		wh.prometheusURL = prometheusURL
		wh.prometheusToken = prometheusToken
	}
	wh.envVars["INGRESS_DOMAIN"], err = wh.ocpMetaAgent.GetDefaultIngressDomain()
	if err != nil {
		log.Fatal("Error obtaining default ingress domain: ", err.Error())
	}
	for k, v := range wh.envVars {
		os.Setenv(k, v)
	}
}

func (wh *WorkloadHelper) GatherMetadata(userMetadata string) error {
	var err error
	wh.Metadata.ClusterMetadata, err = wh.ocpMetaAgent.GetClusterMetadata()
	if err != nil {
		return err
	}
	wh.Metadata.MetricName = clusterMetadataMetric
	if userMetadata != "" {
		userMetadataContent, err := util.ReadUserMetadata(userMetadata)
		if err != nil {
			log.Fatalf("Error reading provided user metadata: %v", err)
		}
		wh.Metadata.UserMetadata = userMetadataContent
	}
	wh.Metadata.Timestamp = time.Now().UTC()
	return nil
}

func (wh *WorkloadHelper) indexMetadata() {
	log.Info("Indexing cluster metadata document")
	wh.Metadata.EndDate = time.Now().UTC()
	msg, err := (*indexer).Index([]interface{}{wh.Metadata}, indexers.IndexingOpts{
		MetricName: wh.Metadata.MetricName,
	})
	if err != nil {
		log.Error(err.Error())
	} else {
		log.Info(msg)
	}
}

func (wh *WorkloadHelper) run(workload, metricsProfile string) {
	metadata := map[string]interface{}{
		"platform":        wh.Metadata.Platform,
		"ocpVersion":      wh.Metadata.OCPVersion,
		"ocpMajorVersion": wh.Metadata.OCPMajorVersion,
		"k8sVersion":      wh.Metadata.K8SVersion,
		"totalNodes":      wh.Metadata.TotalNodes,
		"sdnType":         wh.Metadata.SDNType,
	}
	// Combine provided userMetadata with the regular OCP metadata
	for k, v := range wh.Metadata.UserMetadata {
		metadata[k] = v
	}
	var f io.Reader
	var rc int
	var err error
	var alertM *alerting.AlertManager
	var prometheusClients []*prometheus.Prometheus
	var alertMs []*alerting.AlertManager
	var metricsEndpoints []prometheus.MetricEndpoint
	var embedConfig bool
	configFile := fmt.Sprintf("%s.yml", workload)
	if _, err := os.Stat(configFile); err != nil {
		f, err = util.ReadEmbedConfig(wh.ocpConfig, path.Join(ocpCfgDir, workload, configFile))
		embedConfig = true
		if err != nil {
			log.Fatalf("Error reading configuration file: %v", err.Error())
		}
	} else {
		log.Infof("File %v available in the current directory, using it", configFile)
		f, err = util.ReadConfig(configFile)
		if err != nil {
			log.Fatalf("Error reading configuration file %s: %s", configFile, err)
		}
	}
	configSpec, err = config.Parse(wh.Metadata.UUID, f)
	if err != nil {
		log.Fatal(err)
	}
	if embedConfig {
		configSpec.EmbedFS = wh.ocpConfig
		configSpec.EmbedFSDir = path.Join(ocpCfgDir, workload)
	}
	if wh.metricsEndpoint != "" {
		embedConfig = false
		metrics.DecodeMetricsEndpoint(wh.metricsEndpoint, &metricsEndpoints)
	} else {
		// When benchmark reporting is enabled we hardcode metricsProfile
		if wh.reporting {
			metricsProfile = reportProfile
			for i := range configSpec.GlobalConfig.Measurements {
				configSpec.GlobalConfig.Measurements[i].PodLatencyMetrics = types.Quantiles
			}
		}
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
		auth := prometheus.Auth{
			Token:         metricsEndpoint.Token,
			SkipTLSVerify: true,
		}
		p, err := prometheus.NewPrometheusClient(configSpec, metricsEndpoint.Endpoint, auth, stepSize, metadata, embedConfig)
		if err != nil {
			log.Fatal(err)
		}
		if wh.alerting && configSpec.GlobalConfig.AlertProfile != "" {
			alertM, err = alerting.NewAlertManager(configSpec.GlobalConfig.AlertProfile, wh.Metadata.UUID, indexer, p, embedConfig)
			if err != nil {
				log.Fatal(err)
			}
		}
		prometheusClients = append(prometheusClients, p)
		alertMs = append(alertMs, alertM)
		alertM = nil
	}
	configSpec.GlobalConfig.GCMetrics = (wh.envVars["GC_METRICS"] == "true")
	indexerConfig := configSpec.GlobalConfig.IndexerConfig
	refreshIndexer(indexerConfig)
	rc, err = burner.Run(configSpec, prometheusClients, alertMs, indexer, wh.timeout, metadata)
	if err != nil {
		wh.Metadata.ExecutionErrors = err.Error()
		log.Error(err)
	}
	refreshIndexer(indexerConfig)
	wh.Metadata.Passed = rc == 0
	if indexerConfig.Type != "" {
		wh.indexMetadata()
	}
	log.Info("👋 Exiting kube-burner ", wh.Metadata.UUID)
	os.Exit(rc)
}

// ExtractWorkload extracts the given workload and metrics profile to the current diretory
func (wh *WorkloadHelper) ExtractWorkload(workload, metricsProfile string) error {
	dirContent, err := wh.ocpConfig.ReadDir(path.Join(ocpCfgDir, workload))
	if err != nil {
		return err
	}
	workloadContent, _ := wh.ocpConfig.ReadFile(ocpCfgDir)
	if err = util.CreateFile(fmt.Sprintf("%v.yml", workload), workloadContent); err != nil {
		return err
	}
	for _, f := range dirContent {
		fileContent, _ := wh.ocpConfig.ReadFile(path.Join(ocpCfgDir, workload, f.Name()))
		err := util.CreateFile(f.Name(), fileContent)
		if err != nil {
			return err
		}
	}
	metricsProfileContent, _ := wh.ocpConfig.ReadFile(path.Join(ocpCfgDir, metricsProfile))
	if err = util.CreateFile(metricsProfile, metricsProfileContent); err != nil {
		return err
	}
	reportProfileContent, _ := wh.ocpConfig.ReadFile(path.Join(ocpCfgDir, reportProfile))
	if err = util.CreateFile(reportProfile, reportProfileContent); err != nil {
		return err
	}
	alertsProfileContent, _ := wh.ocpConfig.ReadFile(path.Join(ocpCfgDir, alertsProfile))
	if err = util.CreateFile(alertsProfile, alertsProfileContent); err != nil {
		return err
	}
	return nil
}

// refreshIndexer updates indexer as we have a reference object propagating in code.
func refreshIndexer(indexerConfig indexers.IndexerConfig) {
	var err error
	if indexerConfig.Type != "" {
		log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
		indexer, err = indexers.NewIndexer(indexerConfig)
		if err != nil {
			log.Fatalf("%v indexer: %v", indexerConfig.Type, err.Error())
		}
	}
}
