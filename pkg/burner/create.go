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
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
			unstructured:   uns,
			inputVars:      o.InputVars,
			namespaced:     o.Namespaced,
		}
		if o.Namespaced {
			ex.nsObjects = true
		}
		log.Infof("Job %s: %d iterations with %d %s replicas", jobConfig.Name, jobConfig.JobIterations, obj.replicas, gvk.Kind)
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// RunCreateJob executes a creation job
func (ex *Executor) RunCreateJob() {
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
	if ex.nsObjects && !ex.Config.NamespacedIterations {
		ns = ex.Config.Namespace
		nsLabels["name"] = ns
		if err = createNamespace(ClientSet, ns, nsLabels); err != nil {
			log.Fatal(err.Error())
		}
	}
	t0 := time.Now().Round(time.Second)
	for i := 1; i <= ex.Config.JobIterations; i++ {
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
		for i := 1; i <= ex.Config.JobIterations; i++ {
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
			if errors.IsForbidden(err) {
				log.Fatalf("Authorization error creating %s/%s: %s", obj.GetKind(), obj.GetName(), err)
				return true, err
			} else if errors.IsAlreadyExists(err) {
				log.Errorf("%s/%s in namespace %s already exists", obj.GetKind(), obj.GetName(), ns)
				return true, nil
			} else if err != nil {
				log.Errorf("Error creating object %s/%s in namespace %s: %s", obj.GetKind(), obj.GetName(), ns, err)
			}
			log.Error("Retrying object creation")
			return false, nil
		}
		log.Debugf("Created %s/%s in namespace %s", uns.GetKind(), uns.GetName(), ns)
		return true, err
	}, 1*time.Second, 3, 0, 3)
}
