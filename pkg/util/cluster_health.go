package util

import (
	"context"

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
	nodes, err := clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Errorf("Error getting nodes: %v", err)
		return false
	}

	for _, node := range nodes.Items {
		// Check condition for node health check status as Ready, MemoryPressure, DiskPressure, PIDPressure
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status != "True" {
				isHealthy = false
				log.Errorf("Node %s is not Ready", node.Name)
			}
			if condition.Type != "Ready" && condition.Status != "False" { //nolint:goconst
				isHealthy = false
				log.Errorf("Node %s is experiencing %s", node.Name, condition.Type)
			}

		}
	}
	return isHealthy
}
