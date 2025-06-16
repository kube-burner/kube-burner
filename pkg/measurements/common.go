// Copyright 2024 The Kube-burner Authors.
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

package measurements

import (
	"context"
	"strconv"
	"time"

	kutil "github.com/kube-burner/kube-burner/pkg/util"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
)

func getIntFromLabels(labels map[string]string, key string) int {
	strVal, ok := labels[key]
	if ok {
		val, err := strconv.Atoi(strVal)
		if err == nil {
			return val
		}
	}
	return 0
}

func deployPodInNamespace(clientSet kubernetes.Interface, namespace, podName, image string, command []string) error {
	var podObj = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To[int64](0),
			Containers: []corev1.Container{
				{
					Image:           image,
					Command:         command,
					Name:            podName,
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						RunAsNonRoot:             ptr.To(true),
						SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
						RunAsUser:                ptr.To[int64](1000),
					},
				},
			},
		},
	}

	var err error
	if err = kutil.CreateNamespace(clientSet, namespace, nil, nil); err != nil {
		return err
	}
	if _, err = clientSet.CoreV1().Pods(namespace).Create(context.TODO(), podObj, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Warn(err)
		} else {
			return err
		}
	}
	err = wait.PollUntilContextCancel(context.TODO(), 100*time.Millisecond, true, func(ctx context.Context) (done bool, err error) {
		pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		if pod.Status.Phase != corev1.PodRunning {
			return false, nil
		}
		return true, nil
	})
	return err
}

func getGroupVersionClient(restConfig *rest.Config, gv schema.GroupVersion, types ...runtime.Object) *rest.RESTClient {
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)

	// Add CDI objects to the scheme
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)
	scheme.AddKnownTypes(gv, types...)

	shallowCopy := *restConfig
	shallowCopy.GroupVersion = &gv

	shallowCopy.APIPath = "/apis"
	shallowCopy.NegotiatedSerializer = codecs.WithoutConversion()
	shallowCopy.UserAgent = rest.DefaultKubernetesUserAgent()

	cdiClient, err := rest.RESTClientFor(&shallowCopy)
	if err != nil {
		log.Fatalf("failed to create CDI Client - %v", err)
	}
	return cdiClient
}
