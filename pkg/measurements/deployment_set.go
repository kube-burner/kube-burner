package measurements

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func (p *pprof) needsDaemonSet() bool {
	return len(p.Config.NodeAffinity) > 0
}

func (p *pprof) waitForDaemonSetReady() error {
	ctx := context.TODO()
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	log.Infof("Waiting for DaemonSet %s pods to be ready", types.PprofDaemonSet)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for DaemonSet pods to be ready")
		case <-ticker.C:
			ds, err := p.ClientSet.AppsV1().DaemonSets(types.PprofNamespace).Get(ctx, types.PprofDaemonSet, metav1.GetOptions{})
			if err != nil {
				log.Warnf("Error getting DaemonSet statusanjay7178:feat/node-process-profiling-v2s: %v", err)
				continue
			}

			// Check if all desired pods are ready
			if ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled {
				log.Infof("DaemonSet %s is ready with %d/%d pods", types.PprofDaemonSet, ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)

				// Additional check: verify pods are actually running
				podList, err := p.ClientSet.CoreV1().Pods(types.PprofNamespace).List(ctx, metav1.ListOptions{
					LabelSelector: labels.Set(map[string]string{"app": types.PprofDaemonSet}).String(),
				})
				if err != nil {
					log.Warnf("Error listing pods: %v", err)
					continue
				}

				allRunning := true
				for _, pod := range podList.Items {
					if pod.Status.Phase != corev1.PodRunning {
						allRunning = false
						log.Debugf("Pod %s is in phase %s, waiting...", pod.Name, pod.Status.Phase)
						break
					}
				}

				if allRunning && len(podList.Items) > 0 {
					log.Infof("All %d DaemonSet pods are running and ready", len(podList.Items))
					return nil
				}
			} else {
				log.Debugf("DaemonSet status: %d/%d pods ready", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)
			}
		}
	}
}

func (p *pprof) deployDaemonSet() error {
	ctx := context.TODO()

	// Create namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: types.PprofNamespace,
		},
	}
	_, err := p.ClientSet.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace: %v", err)
	}

	// Create ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      types.PprofSA,
			Namespace: types.PprofNamespace,
		},
	}
	_, err = p.ClientSet.CoreV1().ServiceAccounts(types.PprofNamespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create serviceaccount: %v", err)
	}

	// Create ClusterRole with kubelet permissions
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: types.PprofRole,
		},
		Rules: []rbacv1.PolicyRule{
			{
				NonResourceURLs: []string{"/debug/pprof", "/debug/pprof/*"},
				Verbs:           []string{"get"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"nodes", "nodes/proxy", "nodes/stats", "nodes/metrics"},
				Verbs:     []string{"get", "list"},
			},
		},
	}
	_, err = p.ClientSet.RbacV1().ClusterRoles().Create(ctx, clusterRole, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create clusterrole: %v", err)
	}

	// Create ClusterRoleBinding
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: types.PprofRoleBinding,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     types.PprofRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      types.PprofSA,
				Namespace: types.PprofNamespace,
			},
		},
	}
	_, err = p.ClientSet.RbacV1().ClusterRoleBindings().Create(ctx, clusterRoleBinding, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create clusterrolebinding: %v", err)
	}

	// Create DaemonSet
	ds := p.buildDaemonSet()
	_, err = p.ClientSet.AppsV1().DaemonSets(types.PprofNamespace).Create(ctx, ds, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create daemonset: %v", err)
	}

	log.Infof("pprof-collector: DaemonSet %s deployed in namespace %s", types.PprofDaemonSet, types.PprofNamespace)
	return nil
}

func (p *pprof) buildDaemonSet() *appsv1.DaemonSet {
	privileged := true

	affinity := &corev1.Affinity{}
	affinity.NodeAffinity = &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{},
				},
			},
		},
	}
	for key, value := range p.Config.NodeAffinity {
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(
			affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions,
			corev1.NodeSelectorRequirement{
				Key:      key,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{value},
			},
		)
	}

	hostPathDirectoryOrCreate := corev1.HostPathDirectoryOrCreate
	hostPath := p.Config.PProfDirectory
	if !strings.HasPrefix(hostPath, "/") {
		hostPath = "/mnt/" + hostPath
		log.Warnf("PProfDirectory %s is not an absolute path, using %s", p.Config.PProfDirectory, hostPath)
	}

	// Build volume mounts and volumes based on configuration
	volumeMounts := p.buildVolumeMounts(hostPath)
	volumes := p.buildVolumes(hostPath, hostPathDirectoryOrCreate)

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      types.PprofDaemonSet,
			Namespace: types.PprofNamespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": types.PprofDaemonSet,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": types.PprofDaemonSet,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: types.PprofSA,
					HostNetwork:        true,
					Affinity:           affinity,
					Containers: []corev1.Container{
						{
							Name:  "pprof-collector",
							Image: "curlimages/curl:8.5.0",
							Command: []string{
								"sh",
								"-c",
								"while true; do sleep 3600; done",
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							VolumeMounts: volumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	return ds
}

// buildVolumeMounts creates volume mounts for pprof data, kubelet CA, and optionally CRI-O socket
func (p *pprof) buildVolumeMounts(hostPath string) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{
			Name:      "pprof-data-directory",
			MountPath: hostPath,
		},
		{
			Name:      "kubelet-ca",
			MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
			ReadOnly:  true,
		},
	}

	// Add CRI-O socket mount if any target uses unix socket
	mounts = append(mounts, corev1.VolumeMount{
		Name:      "crio-socket",
		MountPath: "/var/run/crio/crio.sock",
	})
	log.Infof("CRI-O socket mount enabled for unix socket pprof targets")

	return mounts
}

// buildVolumes creates volumes, conditionally adding CRI-O socket if needed
func (p *pprof) buildVolumes(hostPath string, hostPathDirectoryOrCreate corev1.HostPathType) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "pprof-data-directory",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
					Type: &hostPathDirectoryOrCreate,
				},
			},
		},
		{
			Name: "kubelet-ca",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Path: "token",
							},
						},
						{
							ConfigMap: &corev1.ConfigMapProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "kube-root-ca.crt",
								},
								Items: []corev1.KeyToPath{
									{
										Key:  "ca.crt",
										Path: "ca.crt",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Add CRI-O socket volume if any target uses unix socket

	hostPathSocket := corev1.HostPathSocket
	volumes = append(volumes, corev1.Volume{
		Name: "crio-socket",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/var/run/crio/crio.sock",
				Type: &hostPathSocket,
			},
		},
	})

	return volumes
}

func (p *pprof) getPprofNodeTargets(target types.PProftarget) (map[string]string, bool) {
	if target.LabelSelector == nil {
		return map[string]string{"app": types.PprofDaemonSet}, true
	} else {
		return nil, false
	}
}
