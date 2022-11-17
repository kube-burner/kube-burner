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

	"github.com/cloud-bulldozer/kube-burner/log"

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
var clientset *kubernetes.Clientset
var dynamicClient dynamic.Interface

func init() {
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
	clientset = kubernetes.NewForConfigOrDie(restConfig)
	dynamicClient = dynamic.NewForConfigOrDie(restConfig)
}

// GetPrometheus Returns Prometheus URL and valid Bearer token
func GetPrometheus() (string, string, error) {
	prometheusURL, err := getPrometheusURL()
	if err != nil {
		return "", "", err
	}
	prometheusToken, err := getBearerToken()
	if err != nil {
		return "", "", err
	}
	return prometheusURL, prometheusToken, nil
}

// getPrometheusURL Returns a valid prometheus endpoint from the openshift-monitoring/prometheus-k8s route
func getPrometheusURL() (string, error) {
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
	log.Debug("Prometheus endpoint: ", endpoint)
	return endpoint, nil
}

// getBearerToken returns a valid bearer token from the openshift-monitoring/prometheus-k8s service account
func getBearerToken() (string, error) {
	request := authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: pointer.Int64Ptr(int64(tokenExpiration.Seconds())),
		},
	}
	response, err := clientset.CoreV1().ServiceAccounts("openshift-monitoring").CreateToken(context.TODO(), "prometheus-k8s", &request, v1.CreateOptions{})
	if err != nil {
		return "", err
	}
	log.Debug("Bearer token: ", response.Status.Token)
	return response.Status.Token, nil
}

// GetWorkerNodeCount returns the number of worker nodes
func GetWorkerNodeCount() (int, error) {
	nodeList, err := clientset.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{LabelSelector: "node-role.kubernetes.io/worker="})
	log.Debug("Node count: ", len(nodeList.Items))
	return len(nodeList.Items), err
}

// GetCurrentPodCount returns the number of current running pods across all worker nodes
func GetCurrentPodCount() (int, error) {
	var podCount int
	nodeList, err := clientset.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{LabelSelector: "node-role.kubernetes.io/worker="})
	if err != nil {
		return podCount, err
	}
	for _, node := range nodeList.Items {
		podList, err := clientset.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), v1.ListOptions{FieldSelector: "status.phase=Running,spec.nodeName=" + node.Name})
		if err != nil {
			return podCount, err
		}
		podCount += len(podList.Items)
	}
	log.Debug("Current running pod count: ", podCount)
	return podCount, nil
}

// GetInfraDetails returns cluster anme and platform
func GetInfraDetails() (InfraObj, error) {
	var infraJSON InfraObj
	infra, err := dynamicClient.Resource(schema.GroupVersionResource{
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
func GetVersionInfo() (VersionObj, error) {
	var cv clusterVersion
	var versionInfo VersionObj
	version, err := clientset.ServerVersion()
	versionInfo.K8sVersion = version.GitVersion
	if err != nil {
		return versionInfo, err
	}
	clusterVersion, err := dynamicClient.Resource(
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
func GetNodesInfo() (NodeInfo, error) {
	var nodeInfoData NodeInfo
	nodes, err := clientset.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{})
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
				nodeInfoData.WorkerType = node.Labels["node.kubernetes.io/instance-type"]
			case "node-role.kubernetes.io/infra":
				nodeInfoData.InfraCount++
				nodeInfoData.InfraType = node.Labels["node.kubernetes.io/instance-type"]
			}
		}
	}
	return nodeInfoData, err
}

// GetSDNInfo returns SDN type
func GetSDNInfo() (string, error) {
	networkData, err := dynamicClient.Resource(schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "networks",
	}).Get(context.TODO(), "cluster", v1.GetOptions{})
	if err != nil {
		return "", err
	}
	networkType, found, err := unstructured.NestedString(networkData.UnstructuredContent(), "status", "networkType")
	if !found {
		return "", fmt.Errorf("networkType field not found in config.openshift.io/v1/network status")
	}
	return networkType, err
}
