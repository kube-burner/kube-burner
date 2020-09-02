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
	"fmt"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func createNamespaces(clientset *kubernetes.Clientset, config config.Job, uuid string) {
	labels := map[string]string{
		"kube-burner":      config.Name,
		"kube-burner-uuid": uuid,
	}
	for i := 1; i <= config.JobIterations; i++ {
		ns := v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%d", config.Namespace, i), Labels: labels},
		}
		log.Infof("Creating namespace %s", ns.Name)
		_, err := clientset.CoreV1().Namespaces().Create(context.TODO(), &ns, metav1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			log.Warnf("Namespace %s already exists", ns.Name)
		}
		// If !ex.Config.NamespacedIterations we create only one namespace
		if !config.NamespacedIterations {
			break
		}
	}
}

// CleanupNamespaces deletes namespaces with the given selector
func CleanupNamespaces(clientset *kubernetes.Clientset, s *util.Selector) error {
	log.Infof("Deleting namespaces with label %s", s.LabelSelector)
	ns, _ := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{LabelSelector: s.LabelSelector})
	if len(ns.Items) > 0 {
		for _, ns := range ns.Items {
			err := clientset.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{})
			if errors.IsNotFound(err) {
				log.Warnf("Namespace %s not found", ns.Name)
				continue
			}
			if err != nil {
				return err
			}
		}
	}
	ns, _ = clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{LabelSelector: s.LabelSelector})
	if len(ns.Items) > 0 {
		return waitForDeleteNamespaces(clientset, s)
	}
	return nil
}

func waitForDeleteNamespaces(clientset *kubernetes.Clientset, s *util.Selector) error {
	log.Info("Waiting for namespaces to be definitely deleted")
	for {
		ns, err := clientset.CoreV1().Namespaces().List(context.TODO(), s.ListOptions)
		if err != nil {
			return err
		}
		if len(ns.Items) == 0 {
			return nil
		}
		log.Debugf("Waiting for %d namespaces labeled with %s to be deleted", len(ns.Items), s.LabelSelector)
		time.Sleep(1 * time.Second)
	}
}
