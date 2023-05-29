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
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	log "github.com/sirupsen/logrus"

	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

func setupCreateJob(jobConfig config.Job) Executor {
	apiGroupResouces, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		log.Fatal(err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(apiGroupResouces)
	waitClientSet, waitRestConfig, err = config.GetClientSet(jobConfig.QPS*2, jobConfig.Burst*2)
	if err != nil {
		log.Fatalf("Error creating wait clientSet: %s", err.Error())
	}
	waitDynamicClient = dynamic.NewForConfigOrDie(waitRestConfig)
	log.Debugf("Preparing create job: %s", jobConfig.Name)
	ex := Executor{}
	for _, o := range jobConfig.Objects {
		if o.Replicas < 1 {
			log.Warnf("Object template %s has replicas %d < 1, skipping", o.ObjectTemplate, o.Replicas)
			continue
		}
		log.Debugf("Rendering template: %s", o.ObjectTemplate)
		f, err := util.ReadConfig(o.ObjectTemplate)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		t, err := io.ReadAll(f)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		// Deserialize YAML
		uns := &unstructured.Unstructured{}
		cleanTemplate, err := prepareTemplate(t)
		if err != nil {
			log.Fatalf("Error preparing template %s: %s", o.ObjectTemplate, err)
		}
		_, gvk := yamlToUnstructured(cleanTemplate, uns)
		mapping, err := mapper.RESTMapping(gvk.GroupKind())
		if err != nil {
			log.Fatal(err)
		}
		obj := object{
			gvr:        mapping.Resource,
			objectSpec: t,
			kind:       gvk.Kind,
			Object:     o,
		}
		// If any of the objects is namespaced, we configure the job to create namepaces
		if o.Namespaced {
			ex.NamespacedIterations = true
		}
		log.Infof("Job %s: %d iterations with %d %s replicas", jobConfig.Name, jobConfig.JobIterations, obj.Replicas, gvk.Kind)
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// RunCreateJob executes a creation job
func (ex *Executor) RunCreateJob(iterationStart, iterationEnd int) {
	nsLabels := map[string]string{
		"kube-burner-job":  ex.Name,
		"kube-burner-uuid": ex.uuid,
	}
	var wg sync.WaitGroup
	var ns string
	var err error
	log.Infof("Running job %s", ex.Name)
	for label, value := range ex.NamespaceLabels {
		nsLabels[label] = value
	}
	if !ex.NamespacedIterations {
		ns = ex.Namespace
		if err = createNamespace(ClientSet, ns, nsLabels); err != nil {
			log.Fatal(err.Error())
		}
	}
	// We have to sum 1 since the iterations start from 1
	iterationProgress := (iterationEnd - iterationStart + 1) / 10
	percent := 1
	var namespacesCreated = make(map[string]bool)
	var namespacesWaited = make(map[string]bool)
	for i := iterationStart; i <= iterationEnd; i++ {
		if i == iterationStart+iterationProgress*percent {
			log.Infof("%v/%v iterations completed", i-iterationStart, iterationEnd-iterationStart+1)
			percent++
		}
		log.Debugf("Creating object replicas from iteration %d", i)
		if ex.NamespacedIterations {
			ns = ex.generateNamespace(i)
			if !namespacesCreated[ns] {
				if err = createNamespace(ClientSet, ns, nsLabels); err != nil {
					log.Error(err.Error())
					continue
				}
				namespacesCreated[ns] = true
			}
		}
		for objectIndex, obj := range ex.objects {
			ex.replicaHandler(objectIndex, obj, ns, i, &wg)
		}
		if ex.PodWait {
			if !ex.NamespacedIterations || !namespacesWaited[ns] {
				log.Infof("Waiting up to %s for actions to be completed in namespace %s", ex.MaxWaitTimeout, ns)
				wg.Wait()
				ex.waitForObjects(ns)
				namespacesWaited[ns] = true
			}
		}
		if ex.JobIterationDelay > 0 {
			log.Infof("Sleeping for %v", ex.JobIterationDelay)
			time.Sleep(ex.JobIterationDelay)
		}
	}
	// Wait for all replicas to be created
	wg.Wait()
	if ex.WaitWhenFinished && !ex.PodWait {
		log.Infof("Waiting up to %s for actions to be completed", ex.MaxWaitTimeout)
		// This semaphore is used to limit the maximum number of concurrent goroutines
		sem := make(chan int, int(ClientSet.RESTClient().GetRateLimiter().QPS())*2)
		for i := iterationStart; i <= iterationEnd; i++ {
			if ex.NamespacedIterations {
				ns = ex.generateNamespace(i)
				if namespacesWaited[ns] {
					continue
				} else {
					namespacesWaited[ns] = true
				}
			}
			sem <- 1
			wg.Add(1)
			go func(ns string) {
				ex.waitForObjects(ns)
				<-sem
				wg.Done()
			}(ns)
			// Wait for all namespaces to be ready
			if !ex.NamespacedIterations {
				break
			}
		}
		wg.Wait()
	}
}

// Simple integer division on the iteration allows us to batch iterations into
// namespaces. Division means namespaces are populated to their desired number
// of iterations before the next namespace is created.
func (ex *Executor) generateNamespace(iteration int) string {
	nsIndex := iteration / ex.IterationsPerNamespace
	return fmt.Sprintf("%s-%d", ex.Namespace, nsIndex)
}

func (ex *Executor) replicaHandler(objectIndex int, obj object, ns string, iteration int, replicaWg *sync.WaitGroup) {
	var wg sync.WaitGroup
	for r := 1; r <= obj.Replicas; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			var newObject = new(unstructured.Unstructured)
			labels := map[string]string{
				"kube-burner-uuid":  ex.uuid,
				"kube-burner-job":   ex.Name,
				"kube-burner-index": strconv.Itoa(objectIndex),
			}
			templateData := map[string]interface{}{
				jobName:      ex.Name,
				jobIteration: iteration,
				jobUUID:      ex.uuid,
				replica:      r,
			}
			for k, v := range obj.InputVars {
				templateData[k] = v
			}
			ex.limiter.Wait(context.TODO())
			renderedObj, err := util.RenderTemplate(obj.objectSpec, templateData, util.MissingKeyError)
			if err != nil {
				log.Fatalf("Template error in %s: %s", obj.ObjectTemplate, err)
			}
			// Re-decode rendered object
			yamlToUnstructured(renderedObj, newObject)
			for k, v := range newObject.GetLabels() {
				labels[k] = v
			}
			newObject.SetLabels(labels)
			json.Marshal(newObject.Object)
			// replicaWg is necessary because we want to wait for all replicas
			// to be created before running any other action such as verify objects,
			// wait for ready, etc. Without this wait group, running for example,
			// verify objects can lead into a race condition when some objects
			// hasn't been created yet
			replicaWg.Add(1)
			go func() {
				createRequest(obj.gvr, ns, newObject, obj.Namespaced)
				replicaWg.Done()
			}()
		}(r)
	}
	wg.Wait()
}

func createRequest(gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, namespaced bool) {
	var uns *unstructured.Unstructured
	var err error
	RetryWithExponentialBackOff(func() (bool, error) {
		if namespaced {
			uns, err = dynamicClient.Resource(gvr).Namespace(ns).Create(context.TODO(), obj, metav1.CreateOptions{})
		} else {
			uns, err = dynamicClient.Resource(gvr).Create(context.TODO(), obj, metav1.CreateOptions{})
		}
		if err != nil {
			if kerrors.IsUnauthorized(err) {
				log.Fatalf("Authorization error creating %s/%s: %s", obj.GetKind(), obj.GetName(), err)
				return true, err
			} else if kerrors.IsAlreadyExists(err) {
				log.Errorf("%s/%s in namespace %s already exists", obj.GetKind(), obj.GetName(), ns)
				return true, nil
			} else if err != nil {
				log.Errorf("Error creating object %s/%s in namespace %s: %s", obj.GetKind(), obj.GetName(), ns, err)
			}
			log.Error("Retrying object creation")
			return false, nil
		}
		if namespaced {
			log.Debugf("Created %s/%s in namespace %s", uns.GetKind(), uns.GetName(), ns)
		} else {
			log.Debugf("Created %s/%s", uns.GetKind(), uns.GetName(), ns)
		}
		return true, err
	}, 1*time.Second, 3, 0, 3)
}

// RunCreateJobWithChurn executes a churn creation job
func (ex *Executor) RunCreateJobWithChurn() {
	var err error
	// Determine the number of job iterations to churn (min 1)
	numToChurn := int(math.Max(float64(ex.ChurnPercent*ex.JobIterations/100), 1))
	now := time.Now().UTC()
	rand.Seed(now.UnixNano())
	// Create timer for the churn duration
	timer := time.After(ex.ChurnDuration)
	// Patch to label namespaces for deletion
	delPatch := []byte(`[{"op":"add","path":"/metadata/labels/churndelete","value": "delete"}]`)
	for {
		select {
		case <-timer:
			log.Info("Churn job complete")
			return
		default:
			log.Debugf("Next churn loop, workload churning started %v ago", time.Since(now))
		}
		// Max amount of churn is 100% of namespaces
		randStart := 1
		if ex.JobIterations-numToChurn+1 > 0 {
			randStart = rand.Intn(ex.JobIterations-numToChurn+1) + 1
		} else {
			numToChurn = ex.JobIterations
		}
		var namespacesPatched = make(map[string]bool)
		// delete numToChurn namespaces starting at randStart
		for i := randStart; i < numToChurn+randStart; i++ {
			ns := ex.generateNamespace(i)
			if namespacesPatched[ns] {
				continue
			}
			// Label namespaces to be deleted
			_, err = ClientSet.CoreV1().Namespaces().Patch(context.TODO(), ns, types.JSONPatchType, delPatch, metav1.PatchOptions{})
			if err != nil {
				log.Errorf("Error patching namespace %s. Error: %v", ns, err)
			}
			namespacesPatched[ns] = true
		}
		// 1 hour timeout to delete namespaces
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		// Delete namespaces based on the label we added
		CleanupNamespaces(ctx, metav1.ListOptions{LabelSelector: "churndelete=delete"}, true)
		log.Info("Re-creating deleted objects")
		// Re-create objects that were deleted
		ex.RunCreateJob(randStart, numToChurn+randStart-1)
		log.Infof("Sleeping for %v", ex.ChurnDelay)
		time.Sleep(ex.ChurnDelay)
	}
}
