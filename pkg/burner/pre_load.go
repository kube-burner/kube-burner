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
	"slices"
	"strings"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
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
	// Deduplicate images
	slices.Sort(imageList)
	imageList = slices.Compact(imageList)
	if len(imageList) == 0 {
		log.Infof("No images found to pre-load, continuing")
		return nil
	}
	preloadCtx, preloadCancel := context.WithTimeout(ctx, job.PreLoadPeriod)
	defer preloadCancel()
	preloadDSName, desired, err := createDSs(preloadCtx, clientSet, imageList, job.PreLoadNodeLabels)
	if err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	log.Infof("Pre-load: Waiting for images to be pulled on %d nodes (timeout %v)", desired, job.PreLoadPeriod)
	waitForImagePull(preloadCtx, clientSet, preloadDSName, desired, len(imageList))
	if err := util.CleanupNamespacesByLabel(context.Background(), clientSet, "kube-burner.io/job=preload"); err != nil {
		log.Warnf("pre-load: %v", err)
	}
	return nil
}

func waitForImagePull(ctx context.Context, clientSet kubernetes.Interface, daemonSetName string, desired, imageCount int) {
	expectedTotal := desired * imageCount
	seen := make(map[string]struct{})
	discoveredPods := make(map[string]struct{})

	err := wait.PollUntilContextCancel(ctx, preLoadPollInterval, true, func(ctx context.Context) (bool, error) {
		// Re-list only until we've discovered all expected pods.
		if len(discoveredPods) < desired {
			pods, err := clientSet.CoreV1().Pods(preLoadNs).List(ctx, metav1.ListOptions{
				LabelSelector: "app=preload",
			})
			if err != nil {
				return false, fmt.Errorf("listing preload pods in namespace %s: %w", preLoadNs, err)
			}
			for _, pod := range pods.Items {
				if isOwnedByDaemonSet(pod.OwnerReferences, daemonSetName) {
					if _, known := discoveredPods[pod.Name]; !known {
						discoveredPods[pod.Name] = struct{}{}
					}
				}
			}
			return false, err
		}

		if err := countPulledImages(ctx, clientSet, seen, discoveredPods); err != nil {
			return false, err
		}
		pulledTotal := len(seen)
		log.Debugf("Pre-load: %d/%d images pulled across %d nodes", pulledTotal, expectedTotal, desired)
		if pulledTotal == expectedTotal {
			log.Infof("Pre-load: All images pulled on %d nodes", desired)
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		log.Warnf("Pre-load: timed out waiting for images to be pulled in namespace %s: pulled %d/%d", preLoadNs, len(seen), expectedTotal)
	}
}

func countPulledImages(ctx context.Context, clientSet kubernetes.Interface, seen, pendingPods map[string]struct{}) error {
	for podName := range pendingPods {
		pod, err := clientSet.CoreV1().Pods(preLoadNs).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting pod %s in namespace %s: %w", podName, preLoadNs, err)
		}
		var pullCount, pulledCount int
		for _, status := range pod.Status.ContainerStatuses {
			if !strings.HasPrefix(status.Name, "pull-") {
				continue
			}
			pullCount++
			if status.Image != "" {
				seen[podName+"/"+status.Name] = struct{}{}
				pulledCount++
			}
		}
		if pullCount > 0 && pullCount == pulledCount {
			delete(pendingPods, podName)
		}
	}
	return nil
}

func isOwnedByDaemonSet(ownerReferences []metav1.OwnerReference, daemonSetName string) bool {
	for _, ownerReference := range ownerReferences {
		if ownerReference.Kind == string(DaemonSet) && ownerReference.Name == daemonSetName {
			return true
		}
	}
	return false
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

func createDSs(ctx context.Context, clientSet kubernetes.Interface, imageList []string, nodeSelectorLabels map[string]string) (string, int, error) {
	nsLabels := map[string]string{
		config.KubeBurnerLabelJob: "preload",
	}
	if err := util.CreateNamespace(clientSet, preLoadNs, nsLabels, nil); err != nil {
		return "", 0, fmt.Errorf("creating namespace: %v", err)
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
			RevisionHistoryLimit: ptr.To[int32](1),
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
			Command:         []string{"override", "command"},
			ImagePullPolicy: corev1.PullAlways,
		})
	}

	log.Infof("Pre-load: Creating DaemonSet using images %v in namespace %s", imageList, preLoadNs)
	created, err := clientSet.AppsV1().DaemonSets(preLoadNs).Create(ctx, &ds, metav1.CreateOptions{})
	if err != nil {
		return "", 0, err
	}
	var desired int
	err = wait.PollUntilContextCancel(ctx, preLoadPollInterval, true, func(ctx context.Context) (bool, error) {
		ds, err := clientSet.AppsV1().DaemonSets(preLoadNs).Get(ctx, created.Name, metav1.GetOptions{})
		if err != nil {
			log.Errorf("Error getting DaemonSet status in %s: %v", preLoadNs, err)
			return false, nil
		}
		if d := int(ds.Status.DesiredNumberScheduled); d > 0 {
			log.Debugf("Pre-load: DaemonSet scheduled on %d nodes", d)
			desired = d
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return "", 0, fmt.Errorf("timed out waiting for DaemonSet to be scheduled in %s: %w", preLoadNs, err)
	}
	return created.Name, desired, nil
}
