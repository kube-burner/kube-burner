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
	"math/rand"
	"time"

	"maps"

	"github.com/kube-burner/kube-burner/v2/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var preLoadNs = fmt.Sprintf("preload-kube-burner-%05d", rand.Intn(100000))

const preLoadPollInterval = 5 * time.Second

// NestedPod represents a pod nested in a higher level object such as deployment or a daemonset
type NestedPod struct {
	// Spec represents the object spec
	Spec struct {
		Template struct {
			corev1.PodSpec `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

type VMI struct {
	Spec struct {
		Volumes []struct {
			ContainerDisk struct {
				Image string `yaml:"image"`
			} `yaml:"containerDisk"`
		} `yaml:"volumes"`
	} `yaml:"spec"`
}

type NestedVM struct {
	Spec struct {
		Template struct {
			Spec struct {
				Volumes []struct {
					ContainerDisk struct {
						Image string `yaml:"image"`
					} `yaml:"containerDisk"`
				} `yaml:"volumes"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func preLoadImages(ctx context.Context, job JobExecutor, clientSet kubernetes.Interface) error {
	log.Info("Pre-load: images from job ", job.Name)
	imageList, err := getJobImages(job)
	if err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	if len(imageList) == 0 {
		log.Infof("No images found to pre-load, continuing")
		return nil
	}
	preloadCtx, preloadCancel := context.WithTimeout(ctx, job.PreLoadPeriod)
	defer preloadCancel()
	desired, err := createDSs(preloadCtx, clientSet, imageList, job.NamespaceLabels, job.NamespaceAnnotations, job.PreLoadNodeLabels)
	if err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	log.Infof("Pre-load: Waiting for images to be pulled on %d nodes (timeout %v)", desired, job.PreLoadPeriod)
	if err := waitForImagePull(preloadCtx, clientSet, desired, len(imageList)); err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	// 5 minutes should be more than enough to cleanup this namespace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	util.CleanupNamespacesByLabel(ctx, clientSet, "kube-burner-preload=true")
	return nil
}

func waitForImagePull(ctx context.Context, clientSet kubernetes.Interface, desired, imageCount int) error {
	expectedTotal := desired * imageCount
	ticker := time.NewTicker(preLoadPollInterval)
	defer ticker.Stop()
	for {
		pods, err := clientSet.CoreV1().Pods(preLoadNs).List(ctx, metav1.ListOptions{
			LabelSelector: "app=preload",
		})
		if err != nil {
			return fmt.Errorf("listing pods in namespace %s: %v", preLoadNs, err)
		}
		if len(pods.Items) < desired {
			log.Debugf("Pre-load: %d/%d pods created", len(pods.Items), desired)
		} else {
			pulledTotal := 0
			for _, pod := range pods.Items {
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.Name != "pause" && cs.ImageID != "" {
						pulledTotal++
					}
				}
			}
			log.Debugf("Pre-load: %d/%d images pulled across %d pods", pulledTotal, expectedTotal, len(pods.Items))
			if pulledTotal == expectedTotal {
				log.Infof("Pre-load: All images pulled on %d nodes", desired)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for images to be pulled in namespace %s", preLoadNs)
		case <-ticker.C:
		}
	}
}

func getJobImages(job JobExecutor) ([]string, error) {
	var imageList []string
	for _, object := range job.objects {
		renderedObj, err := util.RenderTemplate(object.objectSpec, object.InputVars, util.MissingKeyZero, job.functionTemplates)
		if err != nil {
			return imageList, err
		}
		unsList, _ := yamlToUnstructuredMultiple(object.ObjectTemplate, renderedObj)
		for _, uns := range unsList {
			images := extractImagesFromObject(uns, renderedObj)
			imageList = append(imageList, images...)
		}
	}
	return imageList, nil
}

func extractImagesFromObject(uns *unstructured.Unstructured, renderedObj []byte) []string {
	var imageList []string
	switch uns.GetKind() {
	case Deployment, DaemonSet, ReplicaSet, Job, StatefulSet:
		var pod NestedPod
		runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), &pod)
		for _, i := range pod.Spec.Template.Containers {
			imageList = append(imageList, i.Image)
		}
	case Pod:
		var pod corev1.Pod
		runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), &pod)
		for _, i := range pod.Spec.Containers {
			if i.Image != "" {
				imageList = append(imageList, i.Image)
			}
		}
	case VirtualMachineInstance:
		var vmi VMI
		yaml.Unmarshal(renderedObj, &vmi)
		for _, volume := range vmi.Spec.Volumes {
			if volume.ContainerDisk.Image != "" {
				imageList = append(imageList, volume.ContainerDisk.Image)
			}
		}
	case VirtualMachine, VirtualMachineInstanceReplicaSet:
		var nestedVM NestedVM
		yaml.Unmarshal(renderedObj, &nestedVM)
		for _, volume := range nestedVM.Spec.Template.Spec.Volumes {
			if volume.ContainerDisk.Image != "" {
				imageList = append(imageList, volume.ContainerDisk.Image)
			}
		}
	}
	return imageList
}

func createDSs(ctx context.Context, clientSet kubernetes.Interface, imageList []string, namespaceLabels map[string]string, namespaceAnnotations map[string]string, nodeSelectorLabels map[string]string) (int, error) {
	nsLabels := map[string]string{
		"kube-burner-preload": "true",
	}
	nsAnnotations := make(map[string]string)
	maps.Copy(nsLabels, namespaceLabels)
	maps.Copy(nsAnnotations, namespaceAnnotations)
	if err := util.CreateNamespace(clientSet, preLoadNs, nsLabels, nsAnnotations); err != nil {
		return 0, fmt.Errorf("creating namespace: %v", err)
	}
	dsName := "preload"
	ds := appsv1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       string(DaemonSet),
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: dsName,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": dsName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": dsName},
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					Containers: []corev1.Container{
						{
							Name:  "pause",
							Image: "registry.k8s.io/pause:3.1",
						},
					},
					NodeSelector: nodeSelectorLabels,
				},
			},
		},
	}

	for i, image := range imageList {
		ds.Spec.Template.Spec.Containers = append(ds.Spec.Template.Spec.Containers, corev1.Container{
			Name:            fmt.Sprintf("pull-%d", i),
			Image:           image,
			ImagePullPolicy: corev1.PullAlways,
		})
	}

	log.Infof("Pre-load: Creating DaemonSet using images %v in namespace %s", imageList, preLoadNs)
	_, err := clientSet.AppsV1().DaemonSets(preLoadNs).Create(ctx, &ds, metav1.CreateOptions{})
	if err != nil {
		return 0, err
	}
	ticker := time.NewTicker(preLoadPollInterval)
	defer ticker.Stop()
	for {
		dsList, err := clientSet.AppsV1().DaemonSets(preLoadNs).List(ctx, metav1.ListOptions{})
		if err != nil {
			return 0, fmt.Errorf("getting DaemonSet status in %s: %v", preLoadNs, err)
		}
		if len(dsList.Items) > 0 {
			if desired := int(dsList.Items[0].Status.DesiredNumberScheduled); desired > 0 {
				log.Debugf("Pre-load: DaemonSet scheduled on %d nodes", desired)
				return desired, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("timed out waiting for DaemonSet to be scheduled in %s", preLoadNs)
		case <-ticker.C:
		}
	}
}
