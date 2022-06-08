package burner

import (
	"context"
	"fmt"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

var preLoadNs string = "preload-kube-burner"

// NestedPod represents a pod nested in a higher level object such as deployment or a daemonset
type NestedPod struct {
	// Spec represents the object spec
	Spec struct {
		Template struct {
			corev1.PodSpec `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

func PreLoadImages(job Executor) {
	log.Info("Pre-load: images from job ", job.Config.Name)
	imageList, err := getJobImages(job)
	if err != nil {
		log.Fatal(err)
	}
	err = createDSs(imageList)
	if err != nil {
		log.Fatalf("Pre-load: %v", err)
	}
	log.Infof("Pre-load: Sleeping for %v", job.Config.PreLoadPeriod)
	time.Sleep(job.Config.PreLoadPeriod)
	log.Infof("Pre-load: Deleting namespace %s", preLoadNs)
	CleanupNamespaces(ClientSet, v1.ListOptions{LabelSelector: "kube-burner-preload=true"})
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

func createDSs(imageList []string) error {
	if err := createNamespace(ClientSet, preLoadNs, map[string]string{"kube-burner-preload": "true"}); err != nil {
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
