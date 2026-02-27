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
	"io"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"maps"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

type churnDeletedObject struct {
	object *unstructured.Unstructured
	gvr    schema.GroupVersionResource
}

func (ex *JobExecutor) setupCreateJob() {
	var f io.ReadCloser
	var err error
	log.Debugf("Preparing create job: %s", ex.Name)
	for _, o := range ex.Objects {
		if o.Replicas < 1 {
			log.Warnf("Object template %s has replicas %d < 1, skipping", o.ObjectTemplate, o.Replicas)
			continue
		}
		log.Debugf("Rendering template: %s", o.ObjectTemplate)
		f, err = fileutils.GetWorkloadReader(o.ObjectTemplate, ex.embedCfg)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		defer f.Close()
		t, err := io.ReadAll(f)
		if err != nil {
			log.Fatalf("Error reading template %s: %s", o.ObjectTemplate, err)
		}
		// Deserialize YAML
		cleanTemplate, err := util.CleanupTemplate(t)
		if err != nil {
			log.Fatalf("Error cleaning up template %s: %s", o.ObjectTemplate, err)
		}
		unsList, gvks := yamlToUnstructuredMultiple(o.ObjectTemplate, cleanTemplate)

		// Get GVK for this specific object and Process if multi yaml document
		for i, gvk := range gvks {
			mapping, err := ex.mapper.RESTMapping(gvk.GroupKind())
			// Set Kind on the embedded Object before creating the object struct
			o.Kind = gvk.Kind
			obj := &object{
				objectSpec:    t,
				Object:        o,
				namespace:     unsList[i].GetNamespace(),
				gvk:           gvk,
				documentIndex: i,
			}
			if err == nil {
				obj.gvr = mapping.Resource
				obj.namespaced = mapping.Scope.Name() == meta.RESTScopeNameNamespace
			} else {
				obj.gvr = schema.GroupVersionResource{}
			}
			// Job requires namespaces when one of the objects is namespaced and doesn't have any namespace specified
			if obj.namespaced && obj.namespace == "" {
				ex.nsRequired = true
			}
			log.Debugf("Job %s: %d iterations with %d %s replicas (object %d/%d from template)",
				ex.Name, ex.JobIterations, obj.Replicas, gvk.Kind, i+1, len(gvks))
			ex.objects = append(ex.objects, obj)

		}
	}
}

// RunCreateJob executes a creation job
func (ex *JobExecutor) RunCreateJob(ctx context.Context, iterationStart, iterationEnd int) []error {
	nsAnnotations := make(map[string]string)
	nsLabels := map[string]string{
		config.KubeBurnerLabelJob:   ex.Name,
		config.KubeBurnerLabelUUID:  ex.uuid,
		config.KubeBurnerLabelRunID: ex.runid,
	}
	var wg sync.WaitGroup
	var ns string
	var waitErrors []error
	var namespacesWaited = make(map[string]bool)
	maps.Copy(nsLabels, ex.NamespaceLabels)
	maps.Copy(nsAnnotations, ex.NamespaceAnnotations)
	if ex.nsRequired && !ex.NamespacedIterations {
		ns = ex.createNamespace(ex.Namespace, nsLabels, nsAnnotations)
	}
	// We have to sum 1 since the iterations start from 1
	iterationProgress := (iterationEnd - iterationStart) / 10
	percent := 1
	for i := iterationStart; i < iterationEnd; i++ {
		if ctx.Err() != nil {
			return []error{ctx.Err()}
		}
		if ex.JobIterations > 1 && i == iterationStart+iterationProgress*percent {
			log.Infof("%v/%v iterations completed", i-iterationStart, iterationEnd-iterationStart)
			percent++
		}
		if ex.nsRequired && ex.NamespacedIterations {
			ns = ex.createNamespace(ex.generateNamespace(i), nsLabels, nsAnnotations)
		}
		log.Debugf("Creating object replicas from iteration %d", i)
		for objectIndex, obj := range ex.objects {
			if obj.gvr == (schema.GroupVersionResource{}) {
				// resolveObjectMapping may set ex.nsRequired to true if the object is namespaced but doesn't have a namespace specified
				ex.resolveObjectMapping(obj)
				if ex.nsRequired {
					nsName := ex.Namespace
					if ex.NamespacedIterations {
						nsName = ex.generateNamespace(i)
					}
					ns = ex.createNamespace(nsName, nsLabels, nsAnnotations)
				}
			}
			kbLabels := map[string]string{
				config.KubeBurnerLabelUUID:         ex.uuid,
				config.KubeBurnerLabelJob:          ex.Name,
				config.KubeBurnerLabelIndex:        strconv.Itoa(objectIndex),
				config.KubeBurnerLabelRunID:        ex.runid,
				config.KubeBurnerLabelJobIteration: strconv.Itoa(i),
			}
			ex.objects[objectIndex].LabelSelector = kbLabels
			if obj.RunOnce {
				if !obj.hasRun {
					// this executes only once during the first iteration of an object
					log.Debugf("RunOnce set to %s, so creating object once", obj.ObjectTemplate)
					ex.replicaHandler(ctx, kbLabels, obj, ns, i, &wg)
					obj.hasRun = true
				}
			} else {
				ex.replicaHandler(ctx, kbLabels, obj, ns, i, &wg)
			}
		}
		if !ex.WaitWhenFinished && ex.PodWait {
			if !ex.NamespacedIterations || !namespacesWaited[ns] {
				log.Infof("Waiting up to %s for actions to be completed in namespace %s", ex.MaxWaitTimeout, ns)
				wg.Wait()
				if errs := ex.waitForObjects(ctx, ns); errs != nil {
					waitErrors = append(waitErrors, errs...)
				}
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
	if ex.WaitWhenFinished {
		if errs := ex.waitForCompletion(ctx, iterationStart, iterationEnd, ns, namespacesWaited); len(errs) > 0 {
			waitErrors = append(waitErrors, errs...)
		}
	}
	return waitErrors
}

// Simple integer division on the iteration allows us to batch iterations into
// namespaces. Division means namespaces are populated to their desired number
// of iterations before the next namespace is created.
func (ex *JobExecutor) generateNamespace(iteration int) string {
	nsIndex := iteration / ex.IterationsPerNamespace
	return fmt.Sprintf("%s-%d", ex.Namespace, nsIndex)
}

func (ex *JobExecutor) replicaHandler(ctx context.Context, labels map[string]string, obj *object, ns string, iteration int, replicaWg *sync.WaitGroup) {
	var wg sync.WaitGroup

	for r := 1; r <= obj.Replicas; r++ {
		if ctx.Err() != nil {
			return
		}
		// make a copy of the labels map for each goroutine to prevent panic from concurrent read and write
		copiedLabels := make(map[string]string)
		maps.Copy(copiedLabels, labels)
		copiedLabels[config.KubeBurnerLabelReplica] = strconv.Itoa(r)

		if err := ex.limiter.Wait(ctx); err != nil {
			return
		}
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			newObjects, gvks := ex.renderTemplateForObjectMultiple(obj, iteration, r)
			newObject := newObjects[obj.documentIndex]
			gvk := gvks[obj.documentIndex]

			objectLabels := make(map[string]string)
			maps.Copy(objectLabels, copiedLabels)
			maps.Copy(objectLabels, newObject.GetLabels())
			newObject.SetLabels(objectLabels)
			updateChildLabels(newObject, objectLabels)

			// Before attempting to create an object, this error check confirms the REST mapping exists.
			// If the mapping fails, the function returns early, preventing a futile create attempt.
			// The object's GVK might not have been resolvable during setupCreateJob() - perhaps the
			// corresponding CRD might not be installed or the kube-apiserver isn't reachable at the moment.
			_, err := ex.mapper.RESTMapping(gvk.GroupKind())

			if err != nil {
				log.Errorf("Error getting REST Mapping for %v: %v", gvk, err)
				return
			}
			// replicaWg is necessary because we want to wait for all replicas
			// to be created before running any other action such as verify objects,
			// wait for ready, etc. Without this wait group, running for example,
			// verify objects can lead into a race condition when some objects
			// hasn't been created yet
			replicaWg.Add(1)
			go func(n string) {
				defer replicaWg.Done()
				if !obj.namespaced {
					n = ""
				}
				ex.createRequest(ctx, obj.gvr, n, newObject, ex.MaxWaitTimeout)
			}(ns)
		}(r)
	}
	wg.Wait()
}

// waitForCompletion waits for objects to be ready across the relevant namespaces
func (ex *JobExecutor) waitForCompletion(ctx context.Context, iterationStart, iterationEnd int, ns string, namespacesWaited map[string]bool) []error {
	log.Infof("Waiting up to %s for actions to be completed", ex.MaxWaitTimeout)
	// This semaphore limits the maximum number of concurrent goroutines
	sem := make(chan int, int(ex.restConfig.QPS))
	errChan := make(chan []error, 1)
	var wg sync.WaitGroup
	for i := iterationStart; i < iterationEnd; i++ {
		if ex.nsRequired && ex.NamespacedIterations {
			ns = ex.generateNamespace(i)
			if namespacesWaited[ns] {
				continue
			}
			namespacesWaited[ns] = true
		}
		sem <- 1
		wg.Add(1)
		go func(namespace string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if err := ex.waitForObjects(ctx, namespace); err != nil {
				select {
				case errChan <- err:
				default:
				}
			}
		}(ns)
		// Wait for all namespaces to be ready
		if !ex.NamespacedIterations {
			break
		}
	}
	wg.Wait()
	close(errChan)

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (ex *JobExecutor) createRequest(ctx context.Context, gvr schema.GroupVersionResource, ns string, obj *unstructured.Unstructured, timeout time.Duration) {
	var uns *unstructured.Unstructured
	var err error
	if log.GetLevel() == log.TraceLevel {
		out, _ := yaml.Marshal(obj)
		log.Trace(string(out))
	}
	util.RetryWithExponentialBackOff(func() (bool, error) {
		if ctx.Err() != nil {
			return true, err
		}
		// When the object has a namespace already specified, use it
		if objNs := obj.GetNamespace(); objNs != "" {
			ns = objNs
		}
		if ns != "" {
			uns, err = ex.dynamicClient.Resource(gvr).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
		} else {
			if !ex.nsChurning {
				uns, err = ex.dynamicClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
			} else {
				// Skip non-namespaced objects during namespace churning - they won't be deleted with the namespace
				log.Debugf("Skipping non-namespaced object %s/%s during namespace churning", obj.GetKind(), obj.GetName())
				return true, nil
			}
		}
		if err != nil {
			if kerrors.IsUnauthorized(err) {
				log.Fatalf("Authorization error creating %s/%s: %s", obj.GetKind(), obj.GetName(), err)
				return true, err
			} else if kerrors.IsAlreadyExists(err) {
				if ns != "" {
					log.Errorf("%s/%s in namespace %s already exists", obj.GetKind(), obj.GetName(), ns)
				} else {
					log.Errorf("%s/%s already exists", obj.GetKind(), obj.GetName())
				}
				return true, nil
			} else if kerrors.IsNotFound(err) {
				log.Errorf("Error creating object %s/%s: %v", obj.GetKind(), obj.GetName(), err.Error())
				return true, nil
			}
			if ns != "" {
				log.Errorf("Error creating object %s/%s in namespace %s: %s", obj.GetKind(), obj.GetName(), ns, err)
			} else {
				log.Errorf("Error creating object %s/%s: %s", obj.GetKind(), obj.GetName(), err)
			}
			log.Error("Retrying object creation")
			return false, nil
		}
		atomic.AddInt32(&ex.objectOperations, 1)
		if ns != "" {
			log.Debugf("Created %s/%s in namespace %s", uns.GetKind(), uns.GetName(), ns)
		} else {
			log.Debugf("Created %s/%s", uns.GetKind(), uns.GetName())
		}
		return true, err
	}, 1*time.Second, 3, 0, timeout)
}

func (ex *JobExecutor) createNamespace(ns string, nsLabels, nsAnnotations map[string]string) string {
	if ex.createdNamespaces[ns] {
		return ns
	}
	if err := util.CreateNamespace(ex.clientSet, ns, nsLabels, nsAnnotations); err != nil {
		log.Error(err.Error())
	}
	ex.createdNamespaces[ns] = true
	return ns
}

// RunCreateJobWithChurn executes a churn creation job
func (ex *JobExecutor) RunCreateJobWithChurn(ctx context.Context) []error {
	// Cleanup namespaces based on the labels we added to the objects
	log.Infof("Churning mode: %s", ex.ChurnConfig.Mode)
	switch ex.ChurnConfig.Mode {
	case config.ChurnNamespaces:
		ex.nsChurning = true // Enable namespace churning flag to prevent non namespaced objects to be churned
		if !ex.nsRequired {
			log.Info("No namespaces were created in this job, skipping churning stage")
			return nil
		}
		return ex.churnNamespaces(ctx)
	case config.ChurnObjects:
		ex.churnObjects(ctx)
	}
	return nil
}

func (ex *JobExecutor) churnNamespaces(ctx context.Context) []error {
	var errs []error
	delPatch := fmt.Sprintf(`{"metadata": {"labels": {"%s": ""}}}`, config.KubeBurnerLabelChurnDelete)
	cyclesCount := 0
	now := time.Now().UTC()
	// Create timer for the churn duration
	timer := time.After(ex.ChurnConfig.Duration)
	nsLabels := labels.Set{
		config.KubeBurnerLabelJob:   ex.Name,
		config.KubeBurnerLabelUUID:  ex.uuid,
		config.KubeBurnerLabelRunID: ex.runid,
	}
	// List namespace to churn
	jobNamespaces, err := ex.clientSet.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(nsLabels).String()})
	nsList := jobNamespaces.Items
	if err != nil {
		return []error{fmt.Errorf("unable to list namespaces: %w", err)}
	}
	numToChurn := int(math.Max(float64(ex.ChurnConfig.Percent*len(jobNamespaces.Items)/100), 1))

	for {
		var randStart int
		if ex.ChurnConfig.Duration > 0 {
			select {
			case <-timer:
				log.Info("Churn job complete")
				return errs
			default:
				log.Debugf("Next churn iteration, workload churning started %v ago", time.Since(now))
			}
		}
		// Exit if churn cycles are completed
		if ex.ChurnConfig.Cycles > 0 && cyclesCount >= ex.ChurnConfig.Cycles {
			log.Infof("Reached specified number of churn cycles (%d)", ex.ChurnConfig.Cycles)
			return errs
		}

		// Max amount of churn is 100% of namespaces
		if len(nsList)-numToChurn+1 > 0 {
			randStart = rand.Intn(len(nsList) - numToChurn + 1)
		}
		// We need to perform a natural sort
		sort.Slice(nsList, func(i, j int) bool {
			// Get the number after the last '-'
			numI, _ := strconv.Atoi(nsList[i].Name[strings.LastIndex(nsList[i].Name, "-")+1:])
			numJ, _ := strconv.Atoi(nsList[j].Name[strings.LastIndex(nsList[j].Name, "-")+1:])
			return numI < numJ
		})
		// Add label to namespaces to use label selector for deletion
		for _, ns := range nsList[randStart : numToChurn+randStart] {
			_, err = ex.clientSet.CoreV1().Namespaces().
				Patch(ctx, ns.Name, types.StrategicMergePatchType, []byte(delPatch), metav1.PatchOptions{})
			if err != nil {
				log.Errorf("Error applying label to namespace %s: %v", ns.Name, err)
				errs = append(errs, err)
			}
			ex.createdNamespaces[ns.Name] = false
		}
		// 1 hour timeout to delete namespace
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Hour)
		util.CleanupNamespacesByLabel(cleanupCtx, ex.clientSet, config.KubeBurnerLabelChurnDelete)
		// Re-create objects that were deleted
		log.Infof("Re-creating %d deleted namespaces", numToChurn)
		if jobErrs := ex.RunCreateJob(cleanupCtx, randStart, numToChurn+randStart); jobErrs != nil {
			errs = append(errs, jobErrs...)
		}
		log.Infof("Sleeping for %v", ex.ChurnConfig.Delay)
		time.Sleep(ex.ChurnConfig.Delay)
		cyclesCount++
		cancel()
	}
}

func (ex *JobExecutor) churnObjects(ctx context.Context) {
	cyclesCount := 0
	timer := time.After(ex.ChurnConfig.Duration)
	now := time.Now().UTC()
	var objectList *unstructured.UnstructuredList
	var err error
	for {
		deletedObjects := []churnDeletedObject{}
		if ex.ChurnConfig.Duration > 0 {
			select {
			case <-timer:
				log.Info("Churn job complete")
				return
			default:
				log.Debugf("Next churn iteration, workload churning started %v ago", time.Since(now))
			}
		}
		// Exit if churn cycles are completed
		if ex.ChurnConfig.Cycles > 0 && cyclesCount >= ex.ChurnConfig.Cycles {
			log.Infof("Reached specified number of churn cycles (%d), stopping churn job", ex.ChurnConfig.Cycles)
			return
		}
		log.Infof("Deleting objects")
		for _, obj := range ex.objects {
			// if churning is enabled in the object
			if obj.Churn {
				labelSelector := obj.LabelSelector
				// Remove these labels to list all objects
				delete(labelSelector, config.KubeBurnerLabelJobIteration)
				delete(labelSelector, config.KubeBurnerLabelReplica)
				objectList, err = ex.dynamicClient.Resource(obj.gvr).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
					LabelSelector: labels.FormatLabels(labelSelector),
				})
				if err != nil {
					log.Errorf("Error listing objects: %v", err)
					continue
				}
				numToChurn := int(math.Max(float64(ex.ChurnConfig.Percent*len(objectList.Items)/100), 1))
				randStart := rand.Intn(len(objectList.Items) - numToChurn + 1)
				objectsToDelete := objectList.Items[randStart : numToChurn+randStart]
				for _, objToDelete := range objectsToDelete {
					log.Debugf("Deleting %s/%s", objToDelete.GetKind(), objToDelete.GetName())
					err = ex.dynamicClient.Resource(obj.gvr).Namespace(objToDelete.GetNamespace()).Delete(ctx, objToDelete.GetName(), metav1.DeleteOptions{
						PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
					})
					if err != nil {
						log.Errorf("Error deleting object %s/%s: %v", objToDelete.GetKind(), objToDelete.GetName(), err)
					}
					trimObject(&objToDelete)
					// Store the deleted objects to re-create them later
					deletedObjects = append(deletedObjects, churnDeletedObject{
						object: &objToDelete,
						gvr:    obj.gvr,
					})
				}
			}
		}
		ex.verifyDelete(ctx, deletedObjects)
		ex.reCreateDeletedObjects(ctx, deletedObjects)
		log.Infof("Sleeping for %v", ex.ChurnConfig.Delay)
		time.Sleep(ex.ChurnConfig.Delay)
		cyclesCount++
	}
}

// reCreateDeletedObjects re-creates the deleted objects
func (ex *JobExecutor) reCreateDeletedObjects(ctx context.Context, deletedObjects []churnDeletedObject) {
	var affectedNamespaces = make(map[string]bool)
	log.Infof("Re-creating %d deleted objects", len(deletedObjects))
	var wg sync.WaitGroup
	for _, objectToCreate := range deletedObjects {
		if objectToCreate.object.GetNamespace() != "" {
			affectedNamespaces[objectToCreate.object.GetNamespace()] = true
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ex.limiter.Wait(ctx)
			ex.createRequest(ctx, objectToCreate.gvr, objectToCreate.object.GetNamespace(), objectToCreate.object, ex.MaxWaitTimeout)
		}()
	}
	wg.Wait()
	for namespace := range affectedNamespaces {
		ex.waitForObjects(ctx, namespace)
	}
}

// verifyDelete verifies if the object has been deleted
func (ex *JobExecutor) verifyDelete(ctx context.Context, deletedObjects []churnDeletedObject) {
	for _, obj := range deletedObjects {
		wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (done bool, err error) {
			_, err = ex.dynamicClient.Resource(obj.gvr).Namespace(obj.object.GetNamespace()).Get(ctx, obj.object.GetName(), metav1.GetOptions{})
			if kerrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		})
	}
}

// trimObject trims the object to remove the fields that conflict with the object recreation
func trimObject(obj *unstructured.Unstructured) {
	obj.SetResourceVersion("")
	obj.SetUID("")
	obj.SetSelfLink("")
	obj.SetGeneration(0)
	obj.SetDeletionTimestamp(nil)
	obj.SetDeletionGracePeriodSeconds(nil)
	obj.SetFinalizers(nil)
	obj.SetOwnerReferences(nil)
	obj.SetManagedFields(nil)
	unstructured.RemoveNestedField(obj.Object, "spec", "template", "template", "labels", "controller-uid")
	unstructured.RemoveNestedField(obj.Object, "spec", "selector", "matchLabels", "batch.kubernetes.io/controller-uid")
	unstructured.RemoveNestedField(obj.Object, "spec", "template", "metadata", "labels", "batch.kubernetes.io/controller-uid")
	unstructured.RemoveNestedField(obj.Object, "spec", "template", "metadata", "labels", "controller-uid")
}
