// Copyright 2020 The Kube-burner Authors.
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

package burner

import (
	"context"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

func createNamespace(clientset *kubernetes.Clientset, namespaceName string, nsLabels map[string]string) error {
	ns := v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespaceName, Labels: nsLabels},
	}
	return RetryWithExponentialBackOff(func() (done bool, err error) {
		_, err = clientset.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
		if errors.IsForbidden(err) {
			log.Fatalf("Authorization error creating namespace %s: %s", ns.Name, err)
			return false, err
		}
		if errors.IsAlreadyExists(err) {
			log.Warnf("Namespace %s already exists", ns.Name)
			return true, nil
		} else if err != nil {
			log.Errorf("Unexpected error creating namespace %s: %s", ns.Name, err)
			return false, nil
		}
		log.Debugf("Created namespace: %s", ns.Name)
		return true, err
	})
}

// CleanupNamespaces deletes namespaces with the given selector
func CleanupNamespaces(clientset *kubernetes.Clientset, l metav1.ListOptions) {
	ns, _ := clientset.CoreV1().Namespaces().List(context.TODO(), l)
	if len(ns.Items) > 0 {
		log.Infof("Deleting namespaces with label %s", l.LabelSelector)
		for _, ns := range ns.Items {
			err := clientset.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				log.Warnf("Namespace %s not found", ns.Name)
				continue
			}
			if err != nil {
				log.Errorf("Error cleaning up namespaces: %s", err)
			}
		}
	}
	if len(ns.Items) > 0 {
		waitForDeleteNamespaces(clientset, l)
	}
}

func waitForDeleteNamespaces(clientset *kubernetes.Clientset, l metav1.ListOptions) {
	log.Info("Waiting for namespaces to be definitely deleted")
	wait.PollImmediateInfinite(time.Second, func() (bool, error) {
		ns, err := clientset.CoreV1().Namespaces().List(context.TODO(), l)
		if err != nil {
			return false, err
		}
		if len(ns.Items) == 0 {
			return true, nil
		}
		log.Debugf("Waiting for %d namespaces labeled with %s to be deleted", len(ns.Items), l.LabelSelector)
		return false, nil
	})
}
