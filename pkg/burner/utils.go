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
	"math"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
)

const (
	objectLimit = 500
)

var (
	commonUnderlyingObjectLabelsPath = []string{"spec", "template", "metadata", "labels"}

	kindToLabelPaths = map[string][][]string{
		DaemonSet:      {commonUnderlyingObjectLabelsPath},
		Deployment:     {commonUnderlyingObjectLabelsPath},
		ReplicaSet:     {commonUnderlyingObjectLabelsPath},
		StatefulSet:    {commonUnderlyingObjectLabelsPath, []string{"spec", "selector", "matchLabels"}},
		VirtualMachine: {commonUnderlyingObjectLabelsPath},
	}

	kindToLabelPathsInArray = map[string][][][]string{
		VirtualMachine: {[][]string{
			{"spec", "dataVolumeTemplates"}, {"metadata", "labels"}},
		},
	}
)

func setLabels(obj *unstructured.Unstructured, labels map[string]string, templatePath []string) {
	labelMap, found, _ := unstructured.NestedMap(obj.Object, templatePath...)
	if !found {
		labelMap = make(map[string]any, len(labels))
	}
	for k, v := range labels {
		labelMap[k] = v
	}
	unstructured.SetNestedMap(obj.Object, labelMap, templatePath...)
}

func setLabelsInArray(obj *unstructured.Unstructured, labels map[string]string, arrayPath []string, templatePath []string) {
	array, found, _ := unstructured.NestedSlice(obj.Object, arrayPath...)
	if !found {
		return
	}

	for _, a := range array {
		innerObj := unstructured.Unstructured{}
		innerObj.SetUnstructuredContent(a.(map[string]any))

		setLabels(&innerObj, labels, templatePath)
	}
	unstructured.SetNestedSlice(obj.Object, array, arrayPath...)
}

// Helps to set metadata labels
func setMetadataLabels(obj *unstructured.Unstructured, labels map[string]string) {
	// Will be useful for the resources like Deployments and Replicasets. Because
	// object.SetLabels(labels) doesn't actually set labels for the underlying
	// objects (i.e Pods under deployment/replicastes). So this function should help
	// us achieve that without breaking any of our labeling functionality.
	paths := kindToLabelPaths[obj.GetKind()]
	for _, path := range paths {
		setLabels(obj, labels, path)
	}

	// Do the same for elements stored in array (e.g. dataVolumeTemplates in VirtualMachine)
	arrays := kindToLabelPathsInArray[obj.GetKind()]
	for _, array := range arrays {
		setLabelsInArray(obj, labels, array[0], array[1])
	}
}

func yamlToUnstructured(fileName string, y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvk, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatalf("Error decoding YAML (%s): %s", fileName, err)
	}
	return o, gvk
}

// Verify verifies the number of created objects
func (ex *JobExecutor) Verify() bool {
	var objList *unstructured.UnstructuredList
	var replicas int
	success := true
	log.Info("Verifying created objects")
	for objectIndex, obj := range ex.objects {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kube-burner-uuid=%s,kube-burner-runid=%s,kube-burner-job=%s,kube-burner-index=%d", ex.uuid, ex.runid, ex.Name, objectIndex),
			Limit:         objectLimit,
		}
		err := util.RetryWithExponentialBackOff(func() (done bool, err error) {
			replicas = 0
			for {
				objList, err = ex.dynamicClient.Resource(obj.gvr).Namespace(metav1.NamespaceAll).List(context.TODO(), listOptions)
				if err != nil {
					log.Errorf("Error verifying object: %s", err)
					return false, nil
				}
				replicas += len(objList.Items)
				listOptions.Continue = objList.GetContinue()
				// If continue is not set
				if listOptions.Continue == "" {
					break
				}
			}
			return true, nil
		}, 1*time.Second, 3, 0, 1*time.Minute)
		// Mark success to false if we found an error
		if err != nil {
			success = false
			continue
		}
		var objectsExpected int
		if obj.RunOnce {
			objectsExpected = obj.Replicas
		} else {
			objectsExpected = obj.Replicas * ex.JobIterations
		}
		if replicas != objectsExpected {
			log.Errorf("%s found: %d Expected: %d", obj.gvr.Resource, replicas, objectsExpected)
			success = false
		} else {
			log.Debugf("%s found: %d Expected: %d", obj.gvr.Resource, replicas, objectsExpected)
		}
	}
	return success
}

// RetryWithExponentialBackOff a utility for retrying the given function with exponential backoff.
func RetryWithExponentialBackOff(fn wait.ConditionFunc, duration time.Duration, factor, jitter float64, timeout time.Duration) error {
	steps := int(math.Ceil(math.Log(float64(timeout)/(float64(duration)*(1+jitter))) / math.Log(factor)))
	backoff := wait.Backoff{
		Duration: duration,
		Factor:   factor,
		Jitter:   jitter,
		Steps:    steps,
	}
	return wait.ExponentialBackoff(backoff, fn)
}

// newMapper returns a discovery RESTMapper
func newRESTMapper(discoveryClient *discovery.DiscoveryClient) meta.RESTMapper {
	apiGroupResouces, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Fatal(err)
	}
	return restmapper.NewDiscoveryRESTMapper(apiGroupResouces)
}

func (ex *JobExecutor) Run(ctx context.Context) {
	switch ex.ExecutionMode {
	case config.ExecutionModeParallel:
		ex.runParallel(ctx)
	case config.ExecutionModeSequential:
		ex.runSequential(ctx)
	}
}

func (ex *JobExecutor) getItemListForObject(obj *object) (*unstructured.UnstructuredList, error) {
	var itemList *unstructured.UnstructuredList
	labelSelector := labels.Set(obj.LabelSelector).String()
	listOptions := metav1.ListOptions{
		LabelSelector: labelSelector,
	}

	// Try to find the list of resources by GroupVersionResource.
	err := util.RetryWithExponentialBackOff(func() (done bool, err error) {
		itemList, err = ex.dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
		if err != nil {
			log.Errorf("Error found listing %s labeled with %s: %s", obj.gvr.Resource, labelSelector, err)
			return false, nil
		}
		log.Infof("Found %d %s with selector %s; patching them", len(itemList.Items), obj.gvr.Resource, labelSelector)
		return true, nil
	}, 1*time.Second, 3, 0, ex.MaxWaitTimeout)
	if err != nil {
		return nil, err
	}
	return itemList, nil
}

func (ex *JobExecutor) runSequential(ctx context.Context) {
	for i := range ex.JobIterations {
		for _, obj := range ex.objects {
			if ctx.Err() != nil {
				return
			}
			itemList, err := ex.getItemListForObject(obj)
			if err != nil {
				continue
			}
			var wg sync.WaitGroup
			objectTimeUTC := time.Now().UTC().Unix()
			for _, item := range itemList.Items {
				wg.Add(1)
				go ex.itemHandler(ex, obj, item, i, objectTimeUTC, &wg)
			}
			// Wait for all items in the object
			wg.Wait()

			// If requested, wait for the completion of the specific object
			if ex.ObjectWait {
				ex.waitForObject("", obj)
			}

			if ex.objectFinalizer != nil {
				ex.objectFinalizer(ex, obj)
			}
			// Wait between object
			if ex.ObjectDelay > 0 {
				log.Infof("Sleeping between objects for %v", ex.ObjectDelay)
				time.Sleep(ex.ObjectDelay)
			}
		}
		if ex.WaitWhenFinished {
			ex.waitForObjects("")
		}
		// Wait between job iterations
		if ex.JobIterationDelay > 0 {
			log.Infof("Sleeping between job iterations for %v", ex.JobIterationDelay)
			time.Sleep(ex.JobIterationDelay)
		}
		// Print progress every 10 iterations
		if i%10 == 0 {
			// Skip the first print
			if i > 0 {
				log.Infof("%v/%v iterations completed", i, ex.JobIterations)
			}
		}
	}
}

// runParallel executes all objects for all jobs in parallel
func (ex *JobExecutor) runParallel(ctx context.Context) {
	var wg sync.WaitGroup
	for _, obj := range ex.objects {
		if ctx.Err() != nil {
			return
		}
		itemList, err := ex.getItemListForObject(obj)
		if err != nil {
			continue
		}
		for j := range ex.JobIterations {
			objectTimeUTC := time.Now().UTC().Unix()
			for _, item := range itemList.Items {
				wg.Add(1)
				go ex.itemHandler(ex, obj, item, j, objectTimeUTC, &wg)
			}
		}
	}
	wg.Wait()
	ex.waitForObjects("")
}
