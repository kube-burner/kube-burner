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
	"github.com/kube-burner/kube-burner/pkg/alerting"
	"github.com/kube-burner/kube-burner/pkg/burner"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	alertsProfile         = "alerts.yml"
	ocpCfgDir             = "ocp-config"
	stepSize              = 30 * time.Second
	clusterMetadataMetric = "clusterMetadata"
	reportProfile         = "metrics-report.yml"
)

var configSpec config.Spec

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(config Config, ocpConfig embed.FS) WorkloadHelper {
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
		Config:       config,
		ocpConfig:    ocpConfig,
		OcpMetaAgent: ocpMetadata,
		restConfig:   restConfig,
	}
}

var indexer *indexers.Indexer

// SetKubeBurnerFlags configures the required environment variables and flags for kube-burner
func (wh *WorkloadHelper) SetKubeBurnerFlags() {
	var err error
	if (wh.Config.Indexing || wh.Config.Alerting) && wh.MetricsEndpoint == "" {
		wh.prometheusURL, wh.prometheusToken, err = wh.OcpMetaAgent.GetPrometheus()
		if err != nil {
			log.Fatal("Error obtaining Prometheus information: ", err.Error())
		}
	}
	envVars := map[string]string{
		"ES_SERVER":     wh.EsServer,
		"ES_INDEX":      wh.Esindex,
		"QPS":           fmt.Sprintf("%d", wh.QPS),
		"BURST":         fmt.Sprintf("%d", wh.Burst),
		"GC":            fmt.Sprintf("%v", wh.Gc),
		"GC_METRICS":    fmt.Sprintf("%v", wh.GcMetrics),
		"INDEXING_TYPE": string(wh.Indexer),
	}
	for k, v := range envVars {
		os.Setenv(k, v)
	}
}

func (wh *WorkloadHelper) GatherMetadata(userMetadata string) error {
	var err error
	wh.Metadata.ClusterMetadata, err = wh.OcpMetaAgent.GetClusterMetadata()
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
	wh.Metadata.UUID = wh.UUID
	wh.Metadata.Timestamp = time.Now().UTC()
	return nil
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
	configSpec, err = config.Parse(wh.UUID, f)
	if err != nil {
		log.Fatal(err)
	}
	if embedConfig {
		configSpec.EmbedFS = wh.ocpConfig
		configSpec.EmbedFSDir = path.Join(ocpCfgDir, workload)
	}
	if wh.Config.Indexing {
		indexerConfig := configSpec.GlobalConfig.IndexerConfig
		log.Infof("📁 Creating indexer: %s", indexerConfig.Type)
		indexer, err = indexers.NewIndexer(indexerConfig)
		if err != nil {
			log.Fatalf("%v indexer: %v", indexerConfig.Type, err.Error())
		}
		if wh.MetricsEndpoint != "" {
			embedConfig = false
			metrics.DecodeMetricsEndpoint(wh.MetricsEndpoint, &metricsEndpoints)
		} else {
			regularProfile := prometheus.MetricEndpoint{
				Endpoint:     wh.prometheusURL,
				AlertProfile: alertsProfile,
				Profile:      metricsProfile,
				Token:        wh.prometheusToken,
			}
			reportingProfile := prometheus.MetricEndpoint{
				Endpoint: wh.prometheusURL,
				Profile:  reportProfile,
				Token:    wh.prometheusToken,
			}
			switch ProfileType(wh.ProfileType) {
			case regular:
				metricsEndpoints = append(metricsEndpoints, regularProfile)
			case reporting:
				reportingProfile.AlertProfile = alertsProfile
				metricsEndpoints = append(metricsEndpoints, reportingProfile)
				for i := range configSpec.GlobalConfig.Measurements {
					configSpec.GlobalConfig.Measurements[i].PodLatencyMetrics = types.Quantiles
				}
			case both:
				metricsEndpoints = append(metricsEndpoints, regularProfile, reportingProfile)
			default:
				log.Fatalf("Metrics profile type not supported: %v", wh.ProfileType)
			}
		}
		for _, metricsEndpoint := range metricsEndpoints {
			// Updating the prometheus endpoint actually being used in spec.
			auth := prometheus.Auth{
				Token:         metricsEndpoint.Token,
				SkipTLSVerify: true,
			}
			p, err := prometheus.NewPrometheusClient(configSpec, metricsEndpoint.Endpoint, auth, stepSize, metadata, embedConfig)
			if err != nil {
				log.Fatal(err)
			}
			p.ReadProfile(metricsEndpoint.Profile)
			if err != nil {
				log.Fatal(err)
			}
			if wh.Alerting && metricsEndpoint.AlertProfile != "" {
				alertM, err = alerting.NewAlertManager(metricsEndpoint.AlertProfile, wh.Metadata.UUID, indexer, p, embedConfig)
				if err != nil {
					log.Fatal(err)
				}
			}
			prometheusClients = append(prometheusClients, p)
			alertMs = append(alertMs, alertM)
			alertM = nil
		}
		configSpec.GlobalConfig.GCMetrics = wh.GcMetrics
	}
	rc, err = burner.Run(configSpec, prometheusClients, alertMs, indexer, wh.Timeout, metadata)
	if err != nil {
		wh.Metadata.ExecutionErrors = err.Error()
		log.Error(err)
	}
	wh.Metadata.Passed = rc == 0
	if wh.Indexing {
		IndexMetadata(indexer, wh.Metadata)
	}
	log.Info("👋 Exiting kube-burner ", wh.UUID)
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

// IndexMetadata indexes metadata using given indexer.
func IndexMetadata(indexer *indexers.Indexer, metadata BenchmarkMetadata) {
	log.Info("Indexing cluster metadata document")
	metadata.EndDate = time.Now().UTC()
	msg, err := (*indexer).Index([]interface{}{metadata}, indexers.IndexingOpts{
		MetricName: metadata.MetricName,
	})
	if err != nil {
		log.Error(err.Error())
	} else {
		log.Info(msg)
	}
}
