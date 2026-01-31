package util

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func ClusterHealthCheck(clientSet kubernetes.Interface) {
	log.Infof("🏥 Checking for Cluster Health")
	if ClusterHealthyVanillaK8s(clientSet) {
		log.Infof("Cluster is healthy.")
	} else {
		log.Fatalf("Cluster is not healthy.")
	}
}

func ClusterHealthyVanillaK8s(clientset kubernetes.Interface) bool {
	var isHealthy = true

	// 1) Check API server healthz
	if discovery := clientset.Discovery(); discovery != nil {
		if rc := discovery.RESTClient(); rc != nil {
			if data, err := rc.Get().AbsPath("/healthz").DoRaw(context.Background()); err != nil {
				return false
			} else {
				// common healthy response contains "ok"
				s := string(data)
				if !strings.Contains(strings.ToLower(s), "ok") {
					isHealthy = false
					log.Errorf("apiserver health check failed: %s", s)
				}
			}
		}
	}

	// 2) Check node conditions
	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Errorf("Error getting nodes: %v", err)
		return false
	}
	var problems []string
	for _, node := range nodes.Items {
		var readyFound bool
		// Check condition for node health check status as Ready, MemoryPressure, DiskPressure, PIDPressure
		for _, c := range node.Status.Conditions {
			switch string(c.Type) {
			case "Ready":
				readyFound = true
				if c.Status != "True" {
					problems = append(problems, fmt.Sprintf("node %s not Ready (status=%s)", node.Name, c.Status))
				}
			case "MemoryPressure", "DiskPressure", "PIDPressure":
				if c.Status == "True" {
					problems = append(problems, fmt.Sprintf("node %s has pressure: %s", node.Name, c.Type))
				}
			}
		}
		if !readyFound {
			problems = append(problems, fmt.Sprintf("node %s missing Ready condition", node.Name))
		}
	}
	if len(problems) > 0 {
		isHealthy = false
		log.Errorf("cluster health issues: %s", strings.Join(problems, "; "))
	}

	return isHealthy
}
