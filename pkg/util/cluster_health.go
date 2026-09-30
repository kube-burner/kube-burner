package util

import (
	"context"

	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
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
	ctx := context.Background()

	// 1) API server health
	if !isAPIServerHealthy(ctx, clientset) {
		return false
	}

	// 2) Node health
	return areNodesHealthy(ctx, clientset)
}

func isAPIServerHealthy(ctx context.Context, clientset kubernetes.Interface) bool {
	rc := clientset.Discovery().RESTClient()
	if rc == nil {
		log.Error("discovery REST client is nil")
		return false
	}

	data, err := rc.Get().AbsPath("/healthz").DoRaw(ctx)
	if err != nil {
		log.Errorf("apiserver health check error: %v", err)
		return false
	}

	if string(data) != "ok" {
		log.Errorf("apiserver health check failed: %s", string(data))
		return false
	}

	return true
}

func areNodesHealthy(ctx context.Context, clientset kubernetes.Interface) bool {
	var isHealthy = true
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Errorf("Error getting nodes: %v", err)
		return false
	}

	for _, node := range nodes.Items {
		// Only standard node conditions determine health. Vendors may add
		// conditions whose True status indicates successful provisioning.
		for _, condition := range node.Status.Conditions {
			switch condition.Type {
			case corev1.NodeReady:
				if condition.Status != corev1.ConditionTrue {
					isHealthy = false
					log.Errorf("Node %s is not Ready", node.Name)
				}
			case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure, corev1.NodeNetworkUnavailable:
				if condition.Status != corev1.ConditionFalse {
					isHealthy = false
					log.Errorf("Node %s is experiencing %s", node.Name, condition.Type)
				}
			}
		}
	}
	return isHealthy
}
