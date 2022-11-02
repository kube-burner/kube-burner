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
	"io/ioutil"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func setupCreateJob(jobConfig config.Job) Executor {
	var f io.Reader
	var err error
	log.Infof("Preparing create job: %s", jobConfig.Name)
	selector := util.NewSelector()
	selector.Configure("", fmt.Sprintf("kube-burner-job=%s", jobConfig.Name), "")
	ex := Executor{
		selector: selector,
	}
	for _, o := range jobConfig.Objects {
		if o.Replicas < 1 {
			log.Warnf("Object template %s has replicas %d < 1, skipping", o.ObjectTemplate, o.Replicas)
			continue
		}
		log.Debugf("Processing template: %s", o.ObjectTemplate)
		f, err = util.ReadConfig(o.ObjectTemplate)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		t, err := ioutil.ReadAll(f)
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
		gvr, _ := meta.UnsafeGuessKindToResource(*gvk)
		obj := object{
			gvr:            gvr,
			objectSpec:     t,
			objectTemplate: o.ObjectTemplate,
			replicas:       o.Replicas,
			kind:           gvk.Kind,
			inputVars:      o.InputVars,
			namespaced:     o.Namespaced,
		}
		// If any of the objects is namespaced, we configure the job to create namepaces
		if o.Namespaced {
			ex.nsObjects = true
		}
		log.Infof("Job %s: %d iterations with %d %s replicas", jobConfig.Name, jobConfig.JobIterations, obj.replicas, gvk.Kind)
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// RunCreateJob executes a creation job
func (ex *Executor) RunCreateJob(iterationStart int, iterationEnd int) {
	nsLabels := map[string]string{
		"kube-burner-job":  ex.Config.Name,
		"kube-burner-uuid": ex.uuid,
	}
	var wg sync.WaitGroup
	var ns string
	var err error
	ClientSet, RestConfig, err = config.GetClientSet(ex.Config.QPS, ex.Config.Burst)
	if err != nil {
		log.Fatalf("Error creating clientSet: %s", err)
	}
	if ex.Config.QPS == 0 || ex.Config.Burst == 0 {
		log.Infof("QPS or Burst rates not set, using default client-go values: %v %v", rest.DefaultQPS, rest.DefaultBurst)
	} else {
		log.Infof("QPS: %v", RestConfig.QPS)
		log.Infof("Burst: %v", RestConfig.Burst)
	}
	dynamicClient = dynamic.NewForConfigOrDie(RestConfig)
	log.Infof("Running job %s", ex.Config.Name)
	for label, value := range ex.Config.NamespaceLabels {
		nsLabels[label] = value
	}
	if ex.nsObjects && !ex.Config.NamespacedIterations {
		ns = ex.Config.Namespace
		nsLabels["name"] = ns
		if err = createNamespace(ClientSet, ns, nsLabels); err != nil {
			log.Fatal(err.Error())
		}
	}
	t0 := time.Now().Round(time.Second)
	for i := iterationStart; i <= iterationEnd; i++ {
		log.Debugf("Creating object replicas from iteration %d", i)
		if ex.nsObjects && ex.Config.NamespacedIterations {
			ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
			nsLabels["name"] = ns
			if err = createNamespace(ClientSet, fmt.Sprintf("%s-%d", ex.Config.Namespace, i), nsLabels); err != nil {
				log.Error(err.Error())
				continue
			}
		}
		for objectIndex, obj := range ex.objects {
			wg.Add(1)
			go ex.replicaHandler(objectIndex, obj, ns, i, &wg)
		}
		// Wait for all replicaHandlers to finish before moving forward to next iteration
		wg.Wait()
		if ex.Config.PodWait {
			log.Infof("Waiting up to %s all job actions to be completed", ex.Config.MaxWaitTimeout)
			log.Debugf("Waiting for actions in namespace %v to be completed", ns)
			ex.waitForObjects(ns)
		}
		if ex.Config.JobIterationDelay > 0 {
			log.Infof("Sleeping for %v", ex.Config.JobIterationDelay)
			time.Sleep(ex.Config.JobIterationDelay)
		}
	}
	if ex.Config.WaitWhenFinished && !ex.Config.PodWait {
		wg.Wait()
		log.Infof("Waiting up to %s for actions to be completed", ex.Config.MaxWaitTimeout)
		// This semaphore is used to limit the maximum number of concurrent goroutines
		sem := make(chan int, int(ex.Config.QPS)/2)
		if RestConfig.QPS < 2 {
			sem = make(chan int, int(rest.DefaultQPS)/2)
		}
		for i := iterationStart; i <= iterationEnd; i++ {
			if ex.Config.NamespacedIterations {
				ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
			}
			sem <- 1
			wg.Add(1)
			go func(ns string) {
				ex.waitForObjects(ns)
				<-sem
				wg.Done()
			}(ns)
			// Wait for all namespaces to be ready
			if !ex.Config.NamespacedIterations {
				break
			}
		}
		wg.Wait()
	}
	t1 := time.Now().Round(time.Second)
	log.Infof("Finished the create job in %g seconds", t1.Sub(t0).Seconds())
}

func (ex *Executor) replicaHandler(objectIndex int, obj object, ns string, iteration int, wg *sync.WaitGroup) {
	defer wg.Done()
	for r := 1; r <= obj.replicas; r++ {
		wg.Add(1)
		go func(r int) {
			labels := map[string]string{
				"kube-burner-uuid":  ex.uuid,
				"kube-burner-job":   ex.Config.Name,
				"kube-burner-index": strconv.Itoa(objectIndex),
			}
			templateData := map[string]interface{}{
				jobName:      ex.Config.Name,
				jobIteration: iteration,
				jobUUID:      ex.uuid,
			}
			for k, v := range obj.inputVars {
				templateData[k] = v
			}
			// We are using the same wait group for this inner goroutine, maybe we should consider using a new one
			defer wg.Done()
			ex.limiter.Wait(context.TODO())
			newObject := &unstructured.Unstructured{}
			templateData[replica] = r
			renderedObj, err := util.RenderTemplate(obj.objectSpec, templateData, util.MissingKeyError)
			if err != nil {
				log.Fatalf("Template error in %s: %s", obj.objectTemplate, err)
			}
			// Re-decode rendered object
			yamlToUnstructured(renderedObj, newObject)
			for k, v := range newObject.GetLabels() {
				labels[k] = v
			}
			newObject.SetLabels(labels)
			json.Marshal(newObject.Object)
			createRequest(obj.gvr, ns, newObject, obj.namespaced)
		}(r)
	}
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
			if kerrors.IsForbidden(err) {
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
	log.Info("Starting to Churn job")
	log.Infof("Churn Duration: %v", ex.Config.ChurnDuration)
	log.Infof("Churn Percent: %v", ex.Config.ChurnPercent)
	log.Infof("Churn Delay: %v", ex.Config.ChurnDelay)

	clientSet, _, err := config.GetClientSet(ex.Config.QPS, ex.Config.Burst)
	if err != nil {
		log.Fatalf("Error creating k8s clientSet: %s", err)
	}

	// Determine the number of job iterations to churn (min 1)
	numToChurn := int(math.Max((float64(ex.Config.ChurnPercent) * float64(ex.Config.JobIterations) / float64(100)), 1.0))
	// Create timer for the churn duration
	cTimer := time.NewTimer(ex.Config.ChurnDuration)
	rand.Seed(time.Now().UnixNano())
	// Patch to label namespaces for deletion
	delPatch := []byte(`[{"op":"add","path":"/metadata/labels","value":{"churndelete":"delete"}}]`)

churnComplete:
	for {
		select {
		case <-cTimer.C:
			log.Info("Churn job complete")
			break churnComplete
		default:
			log.Debug("Next churn loop")
		}
		// Max amount of churn is 100% of namespaces
		randStart := 1
		if ex.Config.JobIterations-numToChurn+1 > 0 {
			randStart = rand.Intn(ex.Config.JobIterations-numToChurn+1) + 1
		} else {
			numToChurn = ex.Config.JobIterations
		}
		// delete numToChurn namespaces starting at randStart
		for i := randStart; i < numToChurn+randStart; i++ {
			nsName := fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
			// Label namespaces to be deleted
			_, err = clientSet.CoreV1().Namespaces().Patch(context.TODO(), nsName, types.JSONPatchType, delPatch, metav1.PatchOptions{})
			if err != nil {
				log.Errorf("Error patching namespace %s. Error: %v", nsName, err)
			}

		}
		listOptions := metav1.ListOptions{LabelSelector: "churndelete=delete"}
		// Delete namespaces based on the label we added
		CleanupNamespaces(clientSet, listOptions)

		log.Info("Re-creating deleted objects")
		// Re-create objects that were deleted
		ex.RunCreateJob(randStart, numToChurn+randStart-1)
		time.Sleep(ex.Config.ChurnDelay)
	}
}
