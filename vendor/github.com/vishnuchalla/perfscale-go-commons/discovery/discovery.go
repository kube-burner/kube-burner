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

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vishnuchalla/perfscale-go-commons/logger"

	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/pointer"
)

var tokenExpiration = 10 * time.Hour

type Agent struct {
	clientSet     *kubernetes.Clientset
	dynamicClient dynamic.Interface
}

var discoveryAgent Agent

func NewDiscoveryAgent() Agent {
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".kube", "config")); kubeconfig == "" && !os.IsNotExist(err) {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Fatal(err)
	}
	discoveryAgent = Agent{
		clientSet:     kubernetes.NewForConfigOrDie(restConfig),
		dynamicClient: dynamic.NewForConfigOrDie(restConfig),
	}
	return discoveryAgent
}

// GetPrometheus Returns Prometheus URL and valid Bearer token
func (da *Agent) GetPrometheus() (string, string, error) {
	prometheusURL, err := getPrometheusURL(da.dynamicClient)
	if err != nil {
		return "", "", err
	}
	prometheusToken, err := getBearerToken(da.clientSet)
	if err != nil {
		return "", "", err
	}
	return prometheusURL, prometheusToken, nil
}

// getPrometheusURL Returns a valid prometheus endpoint from the openshift-monitoring/prometheus-k8s route
func getPrometheusURL(dynamicClient dynamic.Interface) (string, error) {
	route, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    routeGroup,
		Version:  routeVersion,
		Resource: routeResource,
	}).Namespace("openshift-monitoring").Get(context.TODO(), "prometheus-k8s", v1.GetOptions{})
	if err != nil {
		return "", err
	}
	prometheusHost, found, err := unstructured.NestedString(route.UnstructuredContent(), "spec", "host")
	if !found {
		return "", fmt.Errorf("host field not found in openshift-monitoring/prometheus-k8s route spec")
	}
	if err != nil {
		return "", err
	}
	endpoint := "https://" + prometheusHost
	logger.Debug("Prometheus endpoint: ", endpoint)
	return endpoint, nil
}

// getBearerToken returns a valid bearer token from the openshift-monitoring/prometheus-k8s service account
func getBearerToken(clientset *kubernetes.Clientset) (string, error) {
	request := authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: pointer.Int64Ptr(int64(tokenExpiration.Seconds())),
		},
	}
	response, err := clientset.CoreV1().ServiceAccounts("openshift-monitoring").CreateToken(context.TODO(), "prometheus-k8s", &request, v1.CreateOptions{})
	if err != nil {
		return "", err
	}
	logger.Debug("Bearer token: ", response.Status.Token)
	return response.Status.Token, nil
}

// GetWorkerNodeCount returns the number of worker nodes
func (da *Agent) GetWorkerNodeCount() (int, error) {
	nodeList, err := da.clientSet.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{LabelSelector: workerNodeSelector})
	logger.Infof("Listed nodes after using selector %s: %d", workerNodeSelector, len(nodeList.Items))
	return len(nodeList.Items), err
}

// GetCurrentPodCount returns the number of current running pods across all worker nodes
func (da *Agent) GetCurrentPodCount() (int, error) {
	var podCount int
	nodeList, err := da.clientSet.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{LabelSelector: workerNodeSelector})
	if err != nil {
		return podCount, err
	}
	for _, node := range nodeList.Items {
		podList, err := da.clientSet.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), v1.ListOptions{FieldSelector: "status.phase=Running,spec.nodeName=" + node.Name})
		if err != nil {
			return podCount, err
		}
		podCount += len(podList.Items)
	}
	logger.Debug("Current running pod count: ", podCount)
	return podCount, nil
}

// GetInfraDetails returns cluster anme and platform
func (da *Agent) GetInfraDetails() (InfraObj, error) {
	var infraJSON InfraObj
	infra, err := da.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "infrastructures",
	}).Get(context.TODO(), "cluster", v1.GetOptions{})
	if err != nil {
		return infraJSON, err
	}
	infraData, _ := infra.MarshalJSON()
	err = json.Unmarshal(infraData, &infraJSON)
	return infraJSON, err
}

// GetVersionInfo obtains OCP and k8s version information
func (da *Agent) GetVersionInfo() (VersionObj, error) {
	var cv clusterVersion
	var versionInfo VersionObj
	version, err := da.clientSet.ServerVersion()
	versionInfo.K8sVersion = version.GitVersion
	if err != nil {
		return versionInfo, err
	}
	clusterVersion, err := da.dynamicClient.Resource(
		schema.GroupVersionResource{
			Group:    "config.openshift.io",
			Version:  "v1",
			Resource: "clusterversions",
		}).Get(context.TODO(), "version", v1.GetOptions{})
	if err != nil {
		return versionInfo, err
	}
	clusterVersionBytes, _ := clusterVersion.MarshalJSON()
	json.Unmarshal(clusterVersionBytes, &cv)
	for _, update := range cv.Status.History {
		if update.State == completedUpdate {
			// obtain the version from the last completed update
			versionInfo.OcpVersion = update.Version
			break
		}
	}
	return versionInfo, err
}

// GetNodesInfo returns node information
func (da *Agent) GetNodesInfo() (NodeInfo, error) {
	var nodeInfoData NodeInfo
	nodes, err := da.clientSet.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		return nodeInfoData, err
	}
	nodeInfoData.TotalNodes = len(nodes.Items)
	for _, node := range nodes.Items {
		for k := range node.Labels {
			switch k {
			case "node-role.kubernetes.io/master":
				nodeInfoData.MasterType = node.Labels["node.kubernetes.io/instance-type"]
			case "node-role.kubernetes.io/worker":
				nodeInfoData.WorkerCount++
				// Discard nodes with infra label
				if _, ok := node.Labels["node-role.kubernetes.io/infra"]; !ok {
					nodeInfoData.WorkerType = node.Labels["node.kubernetes.io/instance-type"]
				}
			case "node-role.kubernetes.io/infra":
				nodeInfoData.InfraCount++
				nodeInfoData.InfraType = node.Labels["node.kubernetes.io/instance-type"]
			}
		}
	}
	return nodeInfoData, err
}

// GetSDNInfo returns SDN type
func (da *Agent) GetSDNInfo() (string, error) {
	networkData, err := da.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "networks",
	}).Get(context.TODO(), "cluster", v1.GetOptions{})
	if err != nil {
		return "", err
	}
	networkType, found, err := unstructured.NestedString(networkData.UnstructuredContent(), "status", "networkType")
	if !found {
		return "", fmt.Errorf("networkType field not found in config.openshift.io/v1/network/networks/cluster status")
	}
	return networkType, err
}

// GetDefaultIngressDomain return default ingress domain
func (da *Agent) GetDefaultIngressDomain() (string, error) {
	ingressController, err := da.dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "operator.openshift.io",
		Version:  "v1",
		Resource: "ingresscontrollers",
	}).Namespace("openshift-ingress-operator").Get(context.TODO(), "default", v1.GetOptions{})
	if err != nil {
		return "", err
	}
	ingressDomain, found, err := unstructured.NestedString(ingressController.UnstructuredContent(), "status", "domain")
	if !found {
		return "", fmt.Errorf("domain field not found in operator.openshift.io/v1/namespaces/openshift-ingress-operator/ingresscontrollers/default status")
	}
	return ingressDomain, err
}
