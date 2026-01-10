package measurements

import (
	"context"
	"fmt"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

func (p *pprof) needsDaemonSet() bool {
	return len(p.Config.NodeAffinity) > 0
}

func (p *pprof) waitForDaemonSetReady() error {
	time.Sleep(time.Second) // There's a small time window where the desired number of pods is 0
	log.Infof("Waiting for DaemonSet/%s to be ready", types.PprofDaemonSet)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	err := wait.PollUntilContextCancel(ctx, 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		ds, err := p.ClientSet.AppsV1().DaemonSets(types.PprofNamespace).Get(context.TODO(), types.PprofDaemonSet, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		if ds.Status.DesiredNumberScheduled != ds.Status.NumberReady {
			return false, nil
		}
		log.Debugf("%d pod replicas of DaemonSet/%s ready", ds.Status.NumberReady, ds.Name)
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("error waiting for DaemonSet/%s to be ready: %v", types.PprofDaemonSet, err)
	}
	return nil
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
	if err != nil {
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
			{
				APIGroups:     []string{"security.openshift.io"}, // Required for OpenShift
				Resources:     []string{"securitycontextconstraints"},
				ResourceNames: []string{"hostaccess"},
				Verbs:         []string{"use"},
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
	if err != nil {
		return fmt.Errorf("failed to create daemonset: %v", err)
	}

	log.Infof("pprof-collector: DaemonSet %s deployed in namespace %s", types.PprofDaemonSet, types.PprofNamespace)
	return nil
}

func (p *pprof) buildDaemonSet() *appsv1.DaemonSet {

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
							Name:    "pprof-collector",
							Image:   "quay.io/kube-burner/fedora-nc:latest",
							Command: []string{"sleep", "inf"},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "pprof-data-directory",
									MountPath: "/mnt/var/run",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "pprof-data-directory",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/var/run",
									Type: ptr.To(corev1.HostPathDirectoryOrCreate),
								},
							},
						},
					},
				},
			},
		},
	}
	return ds
}

func (p *pprof) getPprofNodeTargets(target types.PProftarget) map[string]string {
	if target.LabelSelector == nil {
		return map[string]string{"app": types.PprofDaemonSet}
	} else {
		return nil
	}
}
