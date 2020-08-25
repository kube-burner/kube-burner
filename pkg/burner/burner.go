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
	"sync"
	"time"

	"github.com/rsevilla87/kube-burner/log"
	"github.com/rsevilla87/kube-burner/pkg/config"
	"github.com/rsevilla87/kube-burner/pkg/util"
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
	waitFunc     func(string, *sync.WaitGroup)
	inputVars    map[string]string
}

// Executor contains the information required to execute a job
type Executor struct {
	objects          []object
	Start            time.Time
	End              time.Time
	Config           config.Job
	selector         *util.Selector
	uuid             string
	limiter          *rate.Limiter
	dynamicInterface dynamic.Interface
}

const (
	jobName      = "JobName"
	replica      = "Replica"
	jobIteration = "Iteration"
	UUID         = "UUID"
)

// ClientSet kubernetes clientset
var ClientSet *kubernetes.Clientset

// RestConfig clieng-go rest configuration
var RestConfig *rest.Config

// NewExecutorList Returns a list of executors
func NewExecutorList(uuid string) []Executor {
	var executorList []Executor
	ReadConfig()
	for _, job := range config.ConfigSpec.Jobs {
		ex := getExecutor(job)
		ex.uuid = uuid
		executorList = append(executorList, ex)
	}
	return executorList
}

// ReadConfig Condfigures kube-burner with the given parameters
func ReadConfig() {
	var err error
	var kubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	} else {
		kubeconfig = config.ConfigSpec.GlobalConfig.Kubeconfig
	}
	log.Infof("Using kubeconfig: %s", kubeconfig)
	RestConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("Error configuring kube-burner: %s", err)
	}
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
	RestConfig.QPS = float32(jobConfig.QPS)
	RestConfig.Burst = jobConfig.Burst
	dynamicInterface, err := dynamic.NewForConfig(RestConfig)
	if err != nil {
		log.Fatal(err)
	}
	selector.Configure("", fmt.Sprintf("kube-burner=%s", jobConfig.Name), "")
	ex := Executor{
		Config:           jobConfig,
		selector:         selector,
		limiter:          limiter,
		dynamicInterface: dynamicInterface,
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
		switch kind := obj.unstructured.GetKind(); kind {
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
		default:
			log.Infof("Resource of kind %s has no wait function", kind)
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
	ns := fmt.Sprintf("%s-1", ex.Config.Namespace)
	ex.Start = time.Now().UTC()
	// if ex.Config.Namespaced {
	// }
	createNamespaces(ClientSet, ex.Config, ex.uuid)
	for i := 1; i <= ex.Config.JobIterations; i++ {
		if ex.Config.NamespacedIterations {
			ns = fmt.Sprintf("%s-%d", ex.Config.Namespace, i)
		}
		for _, obj := range ex.objects {
			wg.Add(1)
			go ex.replicaHandler(obj, ns, i, &wg)
		}
		if ex.Config.PodWait {
			// If podWait is enabled, first wait for all replicaHandlers to finish
			wg.Wait()
			for _, obj := range ex.objects {
				// If the object has a wait Function, execute it in its own goroutine
				if obj.waitFunc != nil {
					podWG.Add(1)
					go obj.waitFunc(ns, &podWG)
				}
			}
			log.Info("Waiting for pods to be ready")
			podWG.Wait()
		}
		if ex.Config.JobIterationDelay > 0 {
			log.Infof("Sleeping for %d ms", ex.Config.JobIterationDelay)
			time.Sleep(time.Millisecond * time.Duration(ex.Config.JobIterationDelay))
		}
	}
	wg.Wait()
	ex.End = time.Now().UTC()
}

func yamlToUnstructured(y []byte, uns *unstructured.Unstructured) (runtime.Object, *schema.GroupVersionKind) {
	o, gvr, err := scheme.Codecs.UniversalDeserializer().Decode(y, nil, uns)
	if err != nil {
		log.Fatal(err)
	}
	return o, gvr
}

func (ex *Executor) replicaHandler(obj object, ns string, iteration int, wg *sync.WaitGroup) {
	defer wg.Done()
	tData := map[string]interface{}{
		jobName:      ex.Config.Name,
		jobIteration: iteration,
		UUID:         ex.uuid,
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
		go func(r int) {
			// We are using the same wait group for this inner goroutine, maybe we should consider using a new one
			defer wg.Done()
			wg.Add(1)
			ex.limiter.Wait(context.TODO())
			log.Infof("Creating %s %s on %s", newObject.GetKind(), newObject.GetName(), ns)
			_, err := ex.dynamicInterface.Resource(obj.gvr).Namespace(ns).Create(context.TODO(), newObject, metav1.CreateOptions{})
			if err != nil {
				log.Fatal(err)
			}
		}(r)
	}
}
