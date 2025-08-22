package util

import (
	"context"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// LoadNodesByRole queries cluster nodes and groups them by commonly used roles.
func LoadNodesByRole(clientSet kubernetes.Interface) map[string]corev1.NodeList {
	byRole := map[string]corev1.NodeList{"all": {Items: []corev1.Node{}}}
	if clientSet == nil {
		return byRole
	}
	nodes, err := clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Debugf("Skipping node info injection: failed to list nodes: %v", err)
		return byRole
	}
	add := func(role string, n corev1.Node) {
		nl, exists := byRole[role]
		if !exists {
			nl = corev1.NodeList{Items: []corev1.Node{}}
		}
		nl.Items = append(nl.Items, n)
		byRole[role] = nl
	}

	for _, n := range nodes.Items {
		roles := inferNodeRoles(n.Labels)
		if len(roles) == 0 {
			add("unknown", n)
		} else {
			for _, r := range roles {
				add(r, n)
			}
		}
		add("all", n)
	}
	for role, nl := range byRole {
		sort.Slice(nl.Items, func(i, j int) bool {
			return nl.Items[i].Name < nl.Items[j].Name
		})
		byRole[role] = nl
	}
	return byRole
}

// inferNodeRoles returns a set of roles for a node based on standard labels.
func inferNodeRoles(labels map[string]string) []string {
	rolesSet := map[string]struct{}{}
	for k, v := range labels {
		switch k {
		case "kubernetes.io/role":
			if v != "" {
				rolesSet[v] = struct{}{}
			}
		}
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			r := strings.TrimPrefix(k, "node-role.kubernetes.io/")
			if r == "" && v != "" {
				rolesSet[v] = struct{}{}
			} else if r != "" {
				rolesSet[r] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(rolesSet))
	for r := range rolesSet {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
