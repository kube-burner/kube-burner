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
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	"github.com/cloud-bulldozer/kube-burner/pkg/util"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/scheme"
)

type object struct {
	gvr           schema.GroupVersionResource
	objectSpec    []byte
	replicas      int
	unstructured  *unstructured.Unstructured
	inputVars     map[string]string
	labelSelector map[string]string
}

// Executor contains the information required to execute a job
type Executor struct {
	objects  []object
	Start    time.Time
	End      time.Time
	Config   config.Job
	selector *util.Selector
	uuid     string
	limiter  *rate.Limiter
}

const (
	jobName      = "JobName"
	replica      = "Replica"
	jobIteration = "Iteration"
	jobUUID      = "UUID"
)

// ClientSet kubernetes clientset
var ClientSet *kubernetes.Clientset

var dynamicClient dynamic.Interface

// RestConfig clieng-go rest configuration
var RestConfig *rest.Config

// NewExecutorList Returns a list of executors
func NewExecutorList(uuid string) []Executor {
	var err error
	var executorList []Executor
	RestConfig, err = config.GetRestConfig(0, 0)
	ClientSet = kubernetes.NewForConfigOrDie(RestConfig)
	if err != nil {
		log.Fatalf("Error creating restConfig for kube-burner: %s", err)
	}
	for _, job := range config.ConfigSpec.Jobs {
		ex := getExecutor(job)
		ex.uuid = uuid
		executorList = append(executorList, ex)
	}
	return executorList
}

func getExecutor(jobConfig config.Job) Executor {
	var ex Executor
	if jobConfig.JobType == config.CreationJob {
		ex = setupCreateJob(jobConfig)
	} else if jobConfig.JobType == config.DeletionJob {
		ex = setupDeleteJob(jobConfig)
	} else {
		log.Fatalf("Unknown jobType: %s", jobConfig.JobType)
	}
	// Limits the number of workers to QPS and Burst
	ex.limiter = rate.NewLimiter(rate.Limit(jobConfig.QPS), jobConfig.Burst)
	ex.Config = jobConfig
	return ex
}

func setupCreateJob(jobConfig config.Job) Executor {
	log.Infof("Preparing create job: %s", jobConfig.Name)
	var empty interface{}
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
		f, err := os.Open(o.ObjectTemplate)
		if err != nil {
			log.Fatalf("Error getting gvr: %s", err)
		}
		t, err := ioutil.ReadAll(f)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		// Deserialize YAML
		uns := &unstructured.Unstructured{}
		renderedObj := renderTemplate(t, empty)
		_, gvk := yamlToUnstructured(renderedObj, uns)
		restMapping, err := getGVR(gvk)
		if err != nil {
			log.Fatalf("Error getting GVR: %s", err)
		}
		obj := object{
			gvr:          restMapping.Resource,
			objectSpec:   t,
			replicas:     o.Replicas,
			unstructured: uns,
			inputVars:    o.InputVars,
		}
		log.Infof("Job %s: %d iterations with %d %s replicas", jobConfig.Name, jobConfig.JobIterations, obj.replicas, restMapping.GroupVersionKind.Kind)
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

func setupDeleteJob(jobConfig config.Job) Executor {
	log.Infof("Preparing delete job: %s", jobConfig.Name)
	var ex Executor
	for _, o := range jobConfig.Objects {
		gvk := schema.FromAPIVersionAndKind(o.APIVersion, o.Kind)
		restMapping, err := getGVR(&gvk)
		if err != nil {
			log.Fatalf("Error getting gvr: %s", err)
		}
		if o.LabelSelector == nil {
			log.Fatalf("Empty labelSelectors not allowed with: %s", o.Kind)
		}
		obj := object{
			gvr:           restMapping.Resource,
			labelSelector: o.LabelSelector,
		}
		log.Infof("Job %s: Delete %s with selector %s", jobConfig.Name, restMapping.GroupVersionKind.Kind, labels.Set(obj.labelSelector))
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// find the corresponding GVR (available in *meta.RESTMapping) for gvk
func getGVR(gvk *schema.GroupVersionKind) (*meta.RESTMapping, error) {
	// see https://github.com/kubernetes/kubernetes/issues/86149
	RestConfig.Burst = 0
	// DiscoveryClient queries API server about the resources
	dc, err := discovery.NewDiscoveryClientForConfig(RestConfig)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	return mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

// Cleanup deletes old namespaces from a given job
func (ex *Executor) Cleanup() {
	if ex.Config.Cleanup {
		if err := CleanupNamespaces(ClientSet, ex.selector); err != nil {
			log.Fatalf("Error cleaning up namespaces: %s", err)
		}
	}
}

// RunCreateJob executes a creation job
func (ex *Executor) RunCreateJob() {
	log.Infof("Triggering job: %s", ex.Config.Name)
	ex.Start = time.Now().UTC()
	var wg sync.WaitGroup
	var  string
	var err error
	RestConfig, err = config.GetRestConfig(ex.Config.QPS, ex.Config.Burst)
	if err != nil {
		log.Fatalf("Error creating restConfig for kube-burner: %s", err)
	}
	ClientSet = kubernetes.NewForConfigOrDie(RestConfig)
	log.Infof("QPS: %v", RestConfig.QPS)
	log.Infof("Burst: %v", RestConfig.Burst)
	dynamicClient = dynamic.NewForConfigOrDie(RestConfig)
	if !ex.Config.NamespacedIterations {
		ns = ex.Config.Namespace
		createNamespace(ClientSet, ns, ex.Config, ex.uuid)
	}
	for i := 1; i <= ex.Config.JobIterations; i++ {
		if ex.Config.NamespacedIterations {
			ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
			createNamespace(ClientSet, fmt.Sprintf("%s-%d", ex.Config.Namespace, i), ex.Config, ex.uuid)
		}
		for objectIndex, obj := range ex.objects {
			wg.Add(1)
			go ex.replicaHandler(objectIndex, obj, ns, i, &wg)
		}
		// Wait for all replicaHandlers to finish before move forward to the next interation, only when using namespaced iterations
		if ex.Config.NamespacedIterations {
			wg.Wait()
		}
		if ex.Config.PodWait {
			wg.Wait()
			ex.waitForObjects(ns)
		}
		if ex.Config.JobIterationDelay > 0 {
			log.Infof("Sleeping for %v", ex.Config.JobIterationDelay)
			time.Sleep(ex.Config.JobIterationDelay)
		}
	}
	if ex.Config.WaitWhenFinished && !ex.Config.PodWait {
		wg.Wait()
		for i := 1; i <= ex.Config.JobIterations; i++ {
			if ex.Config.NamespacedIterations {
				ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
			}
			ex.waitForObjects(ns)
			if !ex.Config.NamespacedIterations {
				break
			}
		}
	}
	ex.End = time.Now().UTC()
}

// RunDeleteJob executes a deletion job
func (ex *Executor) RunDeleteJob() {
	ex.Start = time.Now().UTC()
	var wg sync.WaitGroup
	var err error
	RestConfig, err = config.GetRestConfig(ex.Config.QPS, ex.Config.Burst)
	if err != nil {
		log.Fatalf("Error creating restConfig for kube-burner: %s", err)
	}
	ClientSet = kubernetes.NewForConfigOrDie(RestConfig)
	dynamicClient, err = dynamic.NewForConfig(RestConfig)
	if err != nil {
		log.Fatalf("Error creating DynamicClient: %s", err)
	}
	for _, obj := range ex.objects {
		labelSelector := labels.Set(obj.labelSelector).String()
		listOptions := metav1.ListOptions{
			LabelSelector: labelSelector,
		}
		resp, err := dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
		if err != nil {
			log.Errorf("Error found listing %s labeled with %s: %s", obj.gvr.Resource, labelSelector, err)
		}
		for _, item := range resp.Items {
			wg.Add(1)
			go func(item unstructured.Unstructured) {
				defer wg.Done()
				ex.limiter.Wait(context.TODO())
				err := dynamicClient.Resource(obj.gvr).Namespace(item.GetNamespace()).Delete(context.TODO(), item.GetName(), metav1.DeleteOptions{})
				if err != nil {
					log.Errorf("Error found removing %s %s from ns %s: %s", item.GetKind(), item.GetName(), item.GetNamespace(), err)
				} else {
					log.Infof("Removing %s %s from ns %s", item.GetKind(), item.GetName(), item.GetNamespace())
				}
			}(item)
		}
		if ex.Config.WaitForDeletion {
			wg.Wait()
			wait.PollImmediateInfinite(1*time.Second, func() (bool, error) {
				resp, err := dynamicClient.Resource(obj.gvr).List(context.TODO(), listOptions)
				if err != nil {
					return false, err
				}
				if len(resp.Items) > 0 {
					log.Infof("Waiting for %d %s labeled with %s to be deleted", len(resp.Items), obj.gvr.Resource, labelSelector)
					return false, nil
				}
				return true, nil
			})
		}
	}
	ex.End = time.Now().UTC()
}

// Verify verifies the number of created objects
func (ex *Executor) Verify() bool {
	success := true
	log.Info("Verifying created objects")
	for objectIndex, obj := range ex.objects {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kube-burner-uuid=%s,kube-burner-job=%s,kube-burner-index=%d", ex.uuid, ex.Config.Name, objectIndex),
		}
		objList, err := dynamicClient.Resource(obj.gvr).Namespace(metav1.NamespaceAll).List(context.TODO(), listOptions)
		if err != nil {
			log.Errorf("Error verifying object: %s", err)
		}
		objectsExpected := ex.Config.JobIterations * obj.replicas
		if len(objList.Items) != objectsExpected {
			log.Errorf("%s found: %d Expected: %d", obj.gvr.Resource, len(objList.Items), objectsExpected)
			success = false
		} else {
			log.Infof("%s found: %d Expected: %d", obj.gvr.Resource, len(objList.Items), objectsExpected)
		}
	}
	return success
}

func yamlToUnstructured(y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvr, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatalf("Error decoding YAML: %s", err)
	}
	return o, gvr
}

func (ex *Executor) replicaHandler(objectIndex int, obj object, ns string, iteration int, wg *sync.WaitGroup) {
	defer wg.Done()
	labels := map[string]string{
		"kube-burner-uuid":  ex.uuid,
		"kube-burner-job":   ex.Config.Name,
		"kube-burner-index": strconv.Itoa(objectIndex),
	}
	tData := map[string]interface{}{
		jobName:      ex.Config.Name,
		jobIteration: iteration,
		jobUUID:      ex.uuid,
	}
	for k, v := range obj.inputVars {
		tData[k] = v
	}
	for r := 1; r <= obj.replicas; r++ {
		newObject := &unstructured.Unstructured{}
		tData[replica] = r
		renderedObj := renderTemplate(obj.objectSpec, tData)
		// Re-decode rendered object
		yamlToUnstructured(renderedObj, newObject)
		for k, v := range newObject.GetLabels() {
			labels[k] = v
		}
		newObject.SetLabels(labels)
		wg.Add(1)
		go func() {
			// We are using the same wait group for this inner goroutine, maybe we should consider using a new one
			defer wg.Done()
			ex.limiter.Wait(context.TODO())
			_, err := dynamicClient.Resource(obj.gvr).Namespace(ns).Create(context.TODO(), newObject, metav1.CreateOptions{})
			if err != nil {
				log.Errorf("Error creating object: %s", err)
			} else {
				log.Infof("Created %s %s on %s", newObject.GetKind(), newObject.GetName(), ns)
			}
		}()
	}
}
