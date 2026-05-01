// Copyright 2025 The Kube-burner Authors.
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

package measurements

import (
	"fmt"
	"sync"
	"time"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	scaledObjectLatencyMeasurementName          = "scaledObjectLatencyMeasurement"
	scaledObjectLatencyQuantilesMeasurementName = "scaledObjectLatencyQuantilesMeasurement"
)

var (
	supportedScaledObjectConditions = map[string]struct{}{
		"ScaledObjectActive": {},
		"HPACreated":         {},
		"DeploymentReady":    {},
	}

	supportedScaledObjectLatencyJobTypes = map[config.JobType]struct{}{
		config.CreationJob: {},
		config.DeletionJob: {},
	}
)

// scaledObjectMetric holds data about the KEDA ScaledObject lifecycle
type scaledObjectMetric struct {
	// Timestamp is the ScaledObject creationTimestamp (t0)
	Timestamp time.Time `json:"timestamp"`

	// Internal timestamps (unexported, used for latency calculation)
	scaledObjectActive time.Time
	// ScaledObjectActiveLatency is the time from ScaledObject creation to Active=True condition (ms)
	ScaledObjectActiveLatency int `json:"scaledObjectActiveLatency"`

	hpaCreated time.Time
	// HPACreatedLatency is the time from ScaledObject creation to HPA creation (ms)
	HPACreatedLatency int `json:"hpaCreatedLatency"`

	deploymentReady time.Time
	// DeploymentReadyLatency is the time from ScaledObject creation to target Deployment having readyReplicas > 0 (ms)
	DeploymentReadyLatency int `json:"deploymentReadyLatency"`

	MetricName      string `json:"metricName"`
	UUID            string `json:"uuid"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	ScaleTargetName string `json:"scaleTargetName,omitempty"`
	HPAName         string `json:"hpaName,omitempty"`
	JobName         string `json:"jobName,omitempty"`
	Metadata        any    `json:"metadata,omitempty"`
	JobIteration    int    `json:"jobIteration"`
	Replica         int    `json:"replica"`
}

type scaledObjectLatency struct {
	BaseMeasurement
	jobNamespaces map[string]struct{}
}

type scaledObjectLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newScaledObjectLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any, labelSelector string) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedScaledObjectConditions); err != nil {
		return nil, err
	}
	return scaledObjectLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata, labelSelector),
	}, nil
}

func (soFactory scaledObjectLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &scaledObjectLatency{
		BaseMeasurement: soFactory.NewBaseLatency(jobConfig, clientSet, restConfig, scaledObjectLatencyMeasurementName, scaledObjectLatencyQuantilesMeasurementName, embedCfg),
	}
}

// handleCreateScaledObject records the ScaledObject creation time and extracts the scale target name
func (so *scaledObjectLatency) handleCreateScaledObject(obj any) {
	soObj, err := util.ConvertAnyToTyped[kedav1alpha1.ScaledObject](obj)
	if err != nil {
		log.Errorf("scaledObjectLatency: failed to convert to ScaledObject: %v", err)
		return
	}
	soLabels := soObj.GetLabels()
	scaleTargetName := ""
	if soObj.Spec.ScaleTargetRef != nil {
		scaleTargetName = soObj.Spec.ScaleTargetRef.Name
	}
	so.Metrics.LoadOrStore(string(soObj.UID), scaledObjectMetric{
		Timestamp:       soObj.CreationTimestamp.UTC(),
		Namespace:       soObj.Namespace,
		Name:            soObj.Name,
		ScaleTargetName: scaleTargetName,
		MetricName:      scaledObjectLatencyMeasurementName,
		UUID:            so.Uuid,
		JobName:         so.JobConfig.Name,
		Metadata:        so.Metadata,
		JobIteration:    getIntFromLabels(soLabels, config.KubeBurnerLabelJobIteration),
		Replica:         getIntFromLabels(soLabels, config.KubeBurnerLabelReplica),
	})
	log.Debugf("scaledObjectLatency: tracked ScaledObject %s/%s (target: %s)", soObj.Namespace, soObj.Name, scaleTargetName)
}

// handleUpdateScaledObject checks for the Active=True condition using typed KEDA Conditions
func (so *scaledObjectLatency) handleUpdateScaledObject(obj any) {
	soObj, err := util.ConvertAnyToTyped[kedav1alpha1.ScaledObject](obj)
	if err != nil {
		log.Errorf("scaledObjectLatency: failed to convert to ScaledObject: %v", err)
		return
	}
	value, exists := so.Metrics.Load(string(soObj.UID))
	if !exists {
		return
	}
	m := value.(scaledObjectMetric)
	if !m.scaledObjectActive.IsZero() {
		return // already recorded
	}
	// Use typed KEDA Conditions to detect Active=True
	activeCondition := soObj.Status.Conditions.GetActiveCondition()
	if activeCondition.Status == metav1.ConditionTrue {
		m.scaledObjectActive = time.Now().UTC()
		// Also capture the HPA name from the ScaledObject status if available
		if soObj.Status.HpaName != "" && m.HPAName == "" {
			m.HPAName = soObj.Status.HpaName
		}
		log.Debugf("scaledObjectLatency: ScaledObject %s/%s is Active", soObj.Namespace, soObj.Name)
		so.Metrics.Store(string(soObj.UID), m)
	}
}

// handleCreateHPA records when KEDA creates the HPA for a ScaledObject.
// KEDA-created HPAs have ownerReferences pointing to the ScaledObject.
func (so *scaledObjectLatency) handleCreateHPA(obj any) {
	hpa, err := util.ConvertAnyToTyped[autoscalingv2.HorizontalPodAutoscaler](obj)
	if err != nil {
		log.Errorf("scaledObjectLatency: failed to convert to HPA: %v", err)
		return
	}
	// Check if this HPA is in a job namespace
	if _, ok := so.jobNamespaces[hpa.Namespace]; !ok {
		return
	}
	// Find the owning ScaledObject from ownerReferences
	var ownerSOName string
	for _, or := range hpa.OwnerReferences {
		if or.Kind == "ScaledObject" {
			ownerSOName = or.Name
			break
		}
	}
	if ownerSOName == "" {
		return // Not a KEDA-managed HPA
	}
	hpaName := hpa.Name
	hpaNs := hpa.Namespace
	now := time.Now().UTC()
	// Find the matching ScaledObject in our metrics map
	so.Metrics.Range(func(key, value any) bool {
		m := value.(scaledObjectMetric)
		if m.Name == ownerSOName && m.Namespace == hpaNs && m.hpaCreated.IsZero() {
			m.hpaCreated = now
			m.HPAName = hpaName
			so.Metrics.Store(key, m)
			log.Debugf("scaledObjectLatency: HPA %s created for ScaledObject %s/%s", hpaName, hpaNs, ownerSOName)
			return false // stop iteration
		}
		return true
	})
}

// handleUpdateDeployment tracks when the target deployment has readyReplicas > 0
func (so *scaledObjectLatency) handleUpdateDeployment(obj any) {
	deploy, err := util.ConvertAnyToTyped[appsv1.Deployment](obj)
	if err != nil {
		log.Errorf("scaledObjectLatency: failed to convert to Deployment: %v", err)
		return
	}
	// Check if this Deployment is in a job namespace
	if _, ok := so.jobNamespaces[deploy.Namespace]; !ok {
		return
	}
	if deploy.Status.ReadyReplicas <= 0 {
		return
	}
	deployName := deploy.Name
	deployNs := deploy.Namespace
	now := time.Now().UTC()
	// Match against ScaledObjects that target this deployment
	so.Metrics.Range(func(key, value any) bool {
		m := value.(scaledObjectMetric)
		if m.ScaleTargetName == deployName && m.Namespace == deployNs && m.deploymentReady.IsZero() {
			m.deploymentReady = now
			so.Metrics.Store(key, m)
			log.Debugf("scaledObjectLatency: Deployment %s/%s has %d ready replicas", deployNs, deployName, deploy.Status.ReadyReplicas)
			return false // stop iteration
		}
		return true
	})
}

// Start begins the scaledObjectLatency measurement by setting up informers
func (so *scaledObjectLatency) Start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Resolve GVR for ScaledObject CRD
	soGVR, err := util.ResourceToGVR(so.RestConfig, "ScaledObject", "keda.sh/v1alpha1")
	if err != nil {
		return fmt.Errorf("error getting GVR for ScaledObject: %w. Is KEDA installed on the cluster?", err)
	}
	hpaGVR := schema.GroupVersionResource{
		Group:    "autoscaling",
		Version:  "v2",
		Resource: "horizontalpodautoscalers",
	}
	deployGVR := schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}
	// HPA and Deployment watchers won't have kube-burner labels, so we filter by namespace in handlers
	so.jobNamespaces = getJobNamespaces(so.JobConfig)
	so.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(so.RestConfig),
				name:          "scaledObjectWatcher",
				resource:      soGVR,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: so.handleCreateScaledObject,
					UpdateFunc: func(oldObj, newObj any) {
						so.handleUpdateScaledObject(newObj)
					},
				},
				transform: scaledObjectTransformFunc(),
			},
			{
				dynamicClient:      dynamic.NewForConfigOrDie(so.RestConfig),
				name:               "hpaWatcher",
				resource:           hpaGVR,
				skipGlobalSelector: true,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: so.handleCreateHPA,
				},
				transform: hpaTransformFunc(),
			},
			{
				dynamicClient:      dynamic.NewForConfigOrDie(so.RestConfig),
				name:               "deploymentWatcher",
				resource:           deployGVR,
				skipGlobalSelector: true,
				handlers: &cache.ResourceEventHandlerFuncs{
					UpdateFunc: func(oldObj, newObj any) {
						so.handleUpdateDeployment(newObj)
					},
				},
				transform: deploymentTransformFunc(),
			},
		},
	)
	return nil
}

// Collect is a no-op for scaledObjectLatency since we use informers
func (so *scaledObjectLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
}

// Stop stops the measurement, normalizes metrics and calculates quantiles
func (so *scaledObjectLatency) Stop() error {
	return so.StopMeasurement(so.normalizeMetrics, so.getLatency)
}

func (so *scaledObjectLatency) normalizeMetrics() float64 {
	scaledObjectCount := 0
	erroredScaledObjects := 0

	so.Metrics.Range(func(key, value any) bool {
		m := value.(scaledObjectMetric)
		// Skip ScaledObject if it did not reach Active state
		if m.scaledObjectActive.IsZero() {
			log.Warningf("ScaledObject %s/%s latency ignored as it did not reach Active state", m.Namespace, m.Name)
			return true
		}
		errorFlag := 0
		m.ScaledObjectActiveLatency = int(m.scaledObjectActive.Sub(m.Timestamp).Milliseconds())
		if m.ScaledObjectActiveLatency < 0 {
			log.Tracef("ScaledObjectActiveLatency for %s falling under negative case. Setting to 0", m.Name)
			errorFlag = 1
			m.ScaledObjectActiveLatency = 0
		}

		if !m.hpaCreated.IsZero() {
			m.HPACreatedLatency = int(m.hpaCreated.Sub(m.Timestamp).Milliseconds())
			if m.HPACreatedLatency < 0 {
				log.Tracef("HPACreatedLatency for %s falling under negative case. Setting to 0", m.Name)
				errorFlag = 1
				m.HPACreatedLatency = 0
			}
		}

		if !m.deploymentReady.IsZero() {
			m.DeploymentReadyLatency = int(m.deploymentReady.Sub(m.Timestamp).Milliseconds())
			if m.DeploymentReadyLatency < 0 {
				log.Tracef("DeploymentReadyLatency for %s falling under negative case. Setting to 0", m.Name)
				errorFlag = 1
				m.DeploymentReadyLatency = 0
			}
		}

		scaledObjectCount++
		erroredScaledObjects += errorFlag
		so.NormLatencies = append(so.NormLatencies, m)
		return true
	})
	if scaledObjectCount == 0 {
		return 0.0
	}
	return float64(erroredScaledObjects) / float64(scaledObjectCount) * 100.0
}

func (so *scaledObjectLatency) getLatency(normLatency any) map[string]float64 {
	m := normLatency.(scaledObjectMetric)
	return map[string]float64{
		"ScaledObjectActive": float64(m.ScaledObjectActiveLatency),
		"HPACreated":         float64(m.HPACreatedLatency),
		"DeploymentReady":    float64(m.DeploymentReadyLatency),
	}
}

func (so *scaledObjectLatency) IsCompatible() bool {
	_, exists := supportedScaledObjectLatencyJobTypes[so.JobConfig.JobType]
	return exists
}

// scaledObjectTransformFunc preserves the following fields for latency measurements:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: conditions, hpaName
// - spec: scaleTargetRef
func scaledObjectTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}
		minimal := createMinimalUnstructured(u, defaultMetadataTransformOpts())
		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}
		if hpaName, found, _ := unstructured.NestedString(u.Object, "status", "hpaName"); found {
			_ = unstructured.SetNestedField(minimal.Object, hpaName, "status", "hpaName")
		}
		if scaleTargetRef, found, _ := unstructured.NestedMap(u.Object, "spec", "scaleTargetRef"); found {
			_ = unstructured.SetNestedMap(minimal.Object, scaleTargetRef, "spec", "scaleTargetRef")
		}
		return minimal, nil
	}
}

// hpaTransformFunc preserves the following fields:
// - metadata: name, namespace, uid, creationTimestamp, ownerReferences
func hpaTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}
		minimal := createMinimalUnstructured(u, metadataTransformOptions{
			includeNamespace:       true,
			includeOwnerReferences: true,
		})
		return minimal, nil
	}
}

// deploymentTransformFunc preserves the following fields:
// - metadata: name, namespace, uid, creationTimestamp, labels
// - status: readyReplicas
func deploymentTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}
		minimal := createMinimalUnstructured(u, defaultMetadataTransformOpts())
		if readyReplicas, found, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas"); found {
			_ = unstructured.SetNestedField(minimal.Object, readyReplicas, "status", "readyReplicas")
		}
		return minimal, nil
	}
}
