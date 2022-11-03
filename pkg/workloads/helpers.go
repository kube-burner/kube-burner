package workloads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/discovery"
)

type WorkloadHelper struct {
	envVars  map[string]string
	logLevel string
}

type clusterMetadata struct {
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

var metadata clusterMetadata
var kubeburnerCmd []string = []string{"init"}

// NewWorkloadHelper initializes workloadHelper
func NewWorkloadHelper(logLevel string, envVars map[string]string) WorkloadHelper {
	return WorkloadHelper{
		logLevel: logLevel,
		envVars:  envVars,
	}
}

// SetKubeBurnerFlags configures the required environment variables and flags for kube-burner
func (wh *WorkloadHelper) SetKubeBurnerFlags() {
	prometheusURL, prometheusToken, err := discovery.GetPrometheus()
	if err != nil {
		fmt.Println("Error obtaining Prometheus information:", err.Error())
		os.Exit(1)
	}
	if prometheusURL != "" {
		kubeburnerCmd = append(kubeburnerCmd, "-u", prometheusURL)
	}
	if prometheusToken != "" {
		kubeburnerCmd = append(kubeburnerCmd, "-t", prometheusToken)
	}
	kubeburnerCmd = append(kubeburnerCmd, "--log-level", wh.logLevel)
	for k, v := range wh.envVars {
		os.Setenv(k, v)
	}
}

func run(cfg ...string) error {
	kubeburnerCmd := append(kubeburnerCmd, cfg...)
	cmd := exec.Command("kube-burner", kubeburnerCmd...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Println("Running:", strings.Join(cmd.Args, " "))
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func (wh *WorkloadHelper) GatherMetadata() error {
	metadata.UUID = wh.envVars["UUID"]
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
	metadata.Platform = infra.Status.Platform
	metadata.ClusterName = infra.Status.InfrastructureName
	metadata.K8SVersion = version.K8sVersion
	metadata.OCPVersion = version.OcpVersion
	metadata.TotalNodes = nodeInfo.TotalNodes
	metadata.WorkerNodesCount = nodeInfo.WorkerCount
	metadata.InfraNodesCount = nodeInfo.InfraCount
	metadata.MasterNodesType = nodeInfo.MasterType
	metadata.WorkerNodesType = nodeInfo.WorkerType
	metadata.InfraNodesType = nodeInfo.InfraType
	metadata.SDNType = sdnType
	metadata.Timestamp = time.Now().UTC()
	return nil
}

func (wh *WorkloadHelper) IndexMetadata() {
	metadata.EndDate = time.Now().UTC()
	if wh.envVars["ES_SERVER"] == "" {
		fmt.Println("No metadata will be indexed")
	}
	esEndpoint := fmt.Sprintf("%v/%v/document", wh.envVars["ES_SERVER"], wh.envVars["ES_INDEX"])
	body, err := json.Marshal(metadata)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	resp, err := http.Post(esEndpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Println("Error indexing metadata:", err)
		return
	}
	if resp.StatusCode == http.StatusCreated {
		fmt.Println("Cluster metadata indexed correctly")
	}
}
