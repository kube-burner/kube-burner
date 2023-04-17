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

package burner

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const preLoadNs = "preload-kube-burner"

// NestedPod represents a pod nested in a higher level object such as deployment or a daemonset
type NestedPod struct {
	// Spec represents the object spec
	Spec struct {
		Template struct {
			corev1.PodSpec `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

func preLoadImages(job Executor) error {
	log.Info("Pre-load: images from job ", job.Config.Name)
	imageList, err := getJobImages(job)
	if err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	err = createDSs(imageList, job.Config.NamespaceLabels)
	if err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	log.Infof("Pre-load: Sleeping for %v", job.Config.PreLoadPeriod)
	time.Sleep(job.Config.PreLoadPeriod)
	log.Infof("Pre-load: Deleting namespace %s", preLoadNs)
	// 5 minutes should be more than enough to cleanup this namespace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	CleanupNamespaces(ctx, v1.ListOptions{LabelSelector: "kube-burner-preload=true"})
	return nil
}

func getJobImages(job Executor) ([]string, error) {
	var imageList []string
	var unstructuredObject unstructured.Unstructured
	for _, object := range job.objects {
		renderedObj, err := util.RenderTemplate(object.objectSpec, object.inputVars, util.MissingKeyZero)
		if err != nil {
			return imageList, err
		}
		yamlToUnstructured(renderedObj, &unstructuredObject)
		switch unstructuredObject.GetKind() {
		case "Deployment", "DaemonSet", "ReplicaSet", "Job":
			var pod NestedPod
			runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.UnstructuredContent(), &pod)
			for _, i := range pod.Spec.Template.Containers {
				imageList = append(imageList, i.Image)
			}
		case "Pod":
			var pod corev1.Pod
			runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObject.UnstructuredContent(), &pod)
			for _, i := range pod.Spec.Containers {
				if i.Image != "" {
					imageList = append(imageList, i.Image)
				}
			}
		}
	}
	return imageList, nil
}

func createDSs(imageList []string, namespaceLabels map[string]string) error {
	nsLabels := map[string]string{
		"kube-burner-preload": "true",
	}
	for label, value := range namespaceLabels {
		nsLabels[label] = value
	}
	if err := createNamespace(ClientSet, preLoadNs, nsLabels); err != nil {
		log.Fatal(err)
	}
	for i, image := range imageList {
		dsName := fmt.Sprintf("preload-%d", i)
		container := corev1.Container{
			Name:            "kube-burner-rocks",
			ImagePullPolicy: corev1.PullAlways,
			Image:           image,
		}
		ds := appsv1.DaemonSet{
			TypeMeta: v1.TypeMeta{
				Kind:       "DaemonSet",
				APIVersion: "apps/v1",
			},
			ObjectMeta: v1.ObjectMeta{
				GenerateName: dsName,
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &v1.LabelSelector{
					MatchLabels: map[string]string{"app": dsName},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{"app": dsName},
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{container},
						// Only Always restart policy is supported
						Containers: []corev1.Container{
							{
								Name:  "sleep",
								Image: "gcr.io/google_containers/pause-amd64:3.0",
							},
						},
					},
				},
			},
		}
		log.Infof("Pre-load: Creating DaemonSet using image %s in namespace %s", image, preLoadNs)
		_, err := ClientSet.AppsV1().DaemonSets(preLoadNs).Create(context.TODO(), &ds, v1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}
