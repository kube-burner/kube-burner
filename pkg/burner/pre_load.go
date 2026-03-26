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

	"github.com/kube-burner/kube-burner/v2/pkg/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var preLoadNs = fmt.Sprintf("preload-kube-burner-%05d", rand.Intn(100000))

const (
	preLoadPollInterval = 5 * time.Second
	preLoadLabel        = "kube-burner.io/preload=true"
)

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
	nodeCount, err := createPreloadPods(preloadCtx, clientSet, imageList, job.PreLoadNodeLabels)
	if err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	log.Infof("Pre-load: Waiting for images to be pulled on %d nodes (timeout %v)", nodeCount, job.PreLoadPeriod)
	if err := waitForImagePull(preloadCtx, clientSet, imageList, job.PreLoadNodeLabels); err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	if err := util.CleanupNamespacesByLabel(preloadCtx, clientSet, preLoadLabel); err != nil {
		return fmt.Errorf("pre-load: %v", err)
	}
	return nil
}

func waitForImagePull(ctx context.Context, clientSet kubernetes.Interface, imageList []string, nodeSelectorLabels map[string]string) error {
	var labelSelector string
	for k, v := range nodeSelectorLabels {
		if labelSelector != "" {
			labelSelector += ","
		}
		labelSelector += fmt.Sprintf("%s=%s", k, v)
	}
	err := wait.PollUntilContextCancel(ctx, preLoadPollInterval, true, func(ctx context.Context) (bool, error) {
		nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return false, fmt.Errorf("listing nodes: %v", err)
		}
		if len(nodes.Items) == 0 {
			return false, nil
		}
		readyNodes := 0
		for _, node := range nodes.Items {
			nodeImages := make(map[string]struct{})
			for _, img := range node.Status.Images {
				for _, name := range img.Names {
					nodeImages[name] = struct{}{}
				}
			}
			log.Debugf("Pre-load: Node %s has %d images in status", node.Name, len(nodeImages))
			allFound := true
			for _, wanted := range imageList {
				if nodeHasImage(nodeImages, wanted) {
					log.Debugf("Pre-load: Node %s: image %q found", node.Name, wanted)
				} else {
					log.Debugf("Pre-load: Node %s: image %q not found", node.Name, wanted)
					allFound = false
					break
				}
			}
			if allFound {
				readyNodes++
			}
		}
		log.Debugf("Pre-load: %d/%d nodes have all images", readyNodes, len(nodes.Items))
		if readyNodes == len(nodes.Items) {
			log.Infof("Pre-load: All images pulled on %d nodes", readyNodes)
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for images to be pulled: %w", err)
	}
	return nil
}

// nodeHasImage checks whether a node has the given image by looking for it
// in the set of image names from node.status.images. Image names in node status
// typically include the tag or digest, so we check for both exact match and prefix match
// to handle cases like "image:latest" matching "docker.io/library/image:latest".
func nodeHasImage(nodeImages map[string]struct{}, wanted string) bool {
	if _, ok := nodeImages[wanted]; ok {
		return true
	}
	for name := range nodeImages {
		if strings.HasSuffix(name, "/"+wanted) {
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

func createPreloadPods(ctx context.Context, clientSet kubernetes.Interface, imageList []string, nodeSelectorLabels map[string]string) (int, error) {
	nsLabels := map[string]string{"kube-burner.io/preload": "true"}
	if err := util.CreateNamespace(clientSet, preLoadNs, nsLabels, nil); err != nil {
		return 0, fmt.Errorf("creating namespace: %v", err)
	}
	var labelSelector string
	for k, v := range nodeSelectorLabels {
		if labelSelector != "" {
			labelSelector += ","
		}
		labelSelector += fmt.Sprintf("%s=%s", k, v)
	}
	nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return 0, fmt.Errorf("listing nodes: %v", err)
	}
	if len(nodes.Items) == 0 {
		return 0, fmt.Errorf("no nodes found matching labels %v", nodeSelectorLabels)
	}
	containers := []corev1.Container{
		{
			Name:  "pause",
			Image: "registry.k8s.io/pause:3.1",
		},
	}
	for i, image := range imageList {
		containers = append(containers, corev1.Container{
			Name:            fmt.Sprintf("pull-%d", i),
			Image:           image,
			ImagePullPolicy: corev1.PullAlways,
		})
	}
	log.Infof("Pre-load: Creating pods on %d nodes to pull images %v in namespace %s", len(nodes.Items), imageList, preLoadNs)
	for _, node := range nodes.Items {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "preload-",
				Labels:       map[string]string{"app": "preload"},
			},
			Spec: corev1.PodSpec{
				NodeName:                      node.Name,
				RestartPolicy:                 corev1.RestartPolicyNever,
				TerminationGracePeriodSeconds: ptr.To[int64](0),
				Containers:                    containers,
			},
		}
		if _, err := clientSet.CoreV1().Pods(preLoadNs).Create(ctx, &pod, metav1.CreateOptions{}); err != nil {
			return 0, fmt.Errorf("creating preload pod on node %s: %v", node.Name, err)
		}
		log.Debugf("Pre-load: Created pod on node %s", node.Name)
	}
	return len(nodes.Items), nil
}
