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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"
)

type object struct {
	gvr          schema.GroupVersionResource
	objectSpec   []byte
	replicas     int
	unstructured *unstructured.Unstructured
	waitFunc     func(string, *sync.WaitGroup, object)
	inputVars    map[string]string
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
	var executorList []Executor
	ReadConfig(0, 0)
	for _, job := range config.ConfigSpec.Jobs {
		ex := getExecutor(job)
		ex.uuid = uuid
		executorList = append(executorList, ex)
	}
	return executorList
}

// ReadConfig Condfigures kube-burner with the given parameters
func ReadConfig(QPS, burst int) {
	var err error
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if config.ConfigSpec.GlobalConfig.Kubeconfig != "" {
		kubeconfig = config.ConfigSpec.GlobalConfig.Kubeconfig
	}
	if kubeconfig != "" {
		log.Infof("Using kubeconfig: %s", kubeconfig)
		RestConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		log.Info("Using in-cluster configuration")
		RestConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalf("Error configuring kube-burner: %s", err)
	}
	RestConfig.QPS = float32(QPS)
	RestConfig.Burst = burst
	ClientSet, err = kubernetes.NewForConfig(RestConfig)
	if err != nil {
		log.Fatalf("Error configuring kube-burner: %s", err)
	}
}

func getExecutor(jobConfig config.Job) Executor {
	var empty interface{}
	// Limits the number of workers to QPS and Burst
	limiter := rate.NewLimiter(rate.Limit(jobConfig.QPS), jobConfig.Burst)
	selector := util.NewSelector()
	selector.Configure("", fmt.Sprintf("kube-burner=%s", jobConfig.Name), "")
	ex := Executor{
		Config:   jobConfig,
		selector: selector,
		limiter:  limiter,
	}
	for _, o := range jobConfig.Objects {
		if o.Replicas < 1 {
			log.Warnf("Object template %s has replicas %d < 1, skipping", o.ObjectTemplate, o.Replicas)
			continue
		}
		log.Debugf("Processing template: %s", o.ObjectTemplate)
		f, err := os.Open(o.ObjectTemplate)
		if err != nil {
			log.Fatal(err)
		}
		t, err := ioutil.ReadAll(f)
		if err != nil {
			log.Fatal(err)
		}
		// Deserialize YAML
		uns := &unstructured.Unstructured{}
		renderedObj := renderTemplate(t, empty)
		_, gvk := yamlToUnstructured(renderedObj, uns)
		restMapping, err := getGVR(*RestConfig, gvk)
		if err != nil {
			log.Fatal(err)
		}
		obj := object{
			gvr:          restMapping.Resource,
			objectSpec:   t,
			replicas:     o.Replicas,
			unstructured: uns,
			inputVars:    o.InputVars,
		}
		if jobConfig.PodWait || jobConfig.WaitWhenFinished {
			waitFor := true
			if len(jobConfig.WaitFor) > 0 {
				waitFor = false
				for _, kind := range jobConfig.WaitFor {
					if obj.unstructured.GetKind() == kind {
						waitFor = true
						break
					}
				}
			}
			if waitFor {
				kind := obj.unstructured.GetKind()
				switch kind {
				case "Deployment":
					obj.waitFunc = waitForDeployments
				case "ReplicaSet":
					obj.waitFunc = waitForRS
				case "ReplicationController":
					obj.waitFunc = waitForRC
				case "DaemonSet":
					obj.waitFunc = waitForDS
				case "Pod":
					obj.waitFunc = waitForPod
				case "Build":
					obj.waitFunc = waitForBuild
				}
				if obj.waitFunc != nil {
					log.Debugf("Added wait function for %s", kind)
				}
			}
		}
		log.Infof("Job %s: %d iterations with %d %s replicas", jobConfig.Name, jobConfig.JobIterations, obj.replicas, restMapping.GroupVersionKind.Kind)
		ex.objects = append(ex.objects, obj)
	}
	return ex
}

// find the corresponding GVR (available in *meta.RESTMapping) for gvk
func getGVR(restConfig rest.Config, gvk *schema.GroupVersionKind) (*meta.RESTMapping, error) {
	// see https://github.com/kubernetes/kubernetes/issues/86149
	restConfig.Burst = 0
	// DiscoveryClient queries API server about the resources
	dc, err := discovery.NewDiscoveryClientForConfig(&restConfig)
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
			log.Fatal(err)
		}
	}
}

// Run executes the job with the given UUID
func (ex *Executor) Run() {
	var podWG sync.WaitGroup
	var wg sync.WaitGroup
	var err error
	ReadConfig(ex.Config.QPS, ex.Config.Burst)
	dynamicClient, err = dynamic.NewForConfig(RestConfig)
	if err != nil {
		log.Fatal(err)
	}
	ns := fmt.Sprintf("%s-1", ex.Config.Namespace)
	ex.Start = time.Now().UTC()
	createNamespaces(ClientSet, ex.Config, ex.uuid)
	for i := 1; i <= ex.Config.JobIterations; i++ {
		if ex.Config.NamespacedIterations {
			ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
		}
		for objectPosition, obj := range ex.objects {
			wg.Add(1)
			go ex.replicaHandler(objectPosition, obj, ns, i, &wg)
		}
		if ex.Config.PodWait {
			// If podWait is enabled, first wait for all replicaHandlers to finish
			wg.Wait()
			waitForObjects(ex.objects, ns, &podWG)
		}
		if ex.Config.JobIterationDelay > 0 {
			wg.Wait()
			log.Infof("Sleeping for %d ms", ex.Config.JobIterationDelay)
			time.Sleep(time.Millisecond * time.Duration(ex.Config.JobIterationDelay))
		}
	}
	// Wait for all replicaHandlers to finish
	wg.Wait()
	if ex.Config.WaitWhenFinished && !ex.Config.PodWait {
		ns = fmt.Sprintf("%s-1", ex.Config.Namespace)
		for i := 1; i <= ex.Config.JobIterations; i++ {
			if ex.Config.NamespacedIterations {
				ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
			}
			waitForObjects(ex.objects, ns, &podWG)
			if !ex.Config.NamespacedIterations {
				break
			}
		}
	}
	ex.End = time.Now().UTC()
}

// Verify verifies the number of created objects
func (ex *Executor) Verify() bool {
	success := true
	log.Info("Verifying created objects")
	for objectPosition, obj := range ex.objects {
		listOptions := metav1.ListOptions{
			LabelSelector: fmt.Sprintf("kube-burner=%s,objectPosition=%d", ex.uuid, objectPosition),
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
		log.Fatal(err)
	}
	return o, gvr
}

func (ex *Executor) replicaHandler(objectPosition int, obj object, ns string, iteration int, wg *sync.WaitGroup) {
	label := map[string]string{
		"kube-burner":    ex.uuid,
		"objectPosition": strconv.Itoa(objectPosition),
	}
	defer wg.Done()
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
		newObject.SetLabels(label)
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

func waitForObjects(objects []object, ns string, podWG *sync.WaitGroup) {
	var waiting bool = false
	for _, obj := range objects {
		// If the object has a wait function, execute it in its own goroutine
		if obj.waitFunc != nil {
			podWG.Add(1)
			waiting = true
			go obj.waitFunc(ns, podWG, obj)
		}
	}
	if waiting {
		log.Infof("Waiting for actions in namespace %s to be completed", ns)
		podWG.Wait()
	}
}
