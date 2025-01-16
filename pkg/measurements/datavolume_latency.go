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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"kubevirt.io/client-go/kubecli"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/pkg/measurements/types"
)

const (
	dvLatencyMeasurement          = "dvLatencyMeasurement"
	dvLatencyQuantilesMeasurement = "dvLatencyQuantilesMeasurement"
)

var (
	supportedDvConditions = map[string]struct{}{
		string(cdiv1beta1.DataVolumeBound):   {},
		string(cdiv1beta1.DataVolumeRunning): {},
		string(cdiv1beta1.DataVolumeReady):   {},
	}
)

// dvMetric holds data about DataVolume creation process
type dvMetric struct {
	// Timestamp filed is very important the the elasticsearch indexing and represents the first creation time that we track (i.e., vm or vmi)
	Timestamp time.Time `json:"timestamp"`

	dvBound          time.Time
	DVBoundLatency   int `json:"dvBoundLatency"`
	dvRunning        time.Time
	DVRunningLatency int `json:"dvRunningLatency"`
	dvReady          time.Time
	DVReadyLatency   int `json:"dvReadyLatency"`

	MetricName   string      `json:"metricName"`
	UUID         string      `json:"uuid"`
	Namespace    string      `json:"namespace"`
	Name         string      `json:"dvName"`
	JobName      string      `json:"jobName,omitempty"`
	JobIteration int         `json:"jobIteration"`
	Replica      int         `json:"replica"`
	Metadata     interface{} `json:"metadata,omitempty"`
}

type dvLatency struct {
	baseLatencyMeasurement

	watcher          *metrics.Watcher
	metrics          sync.Map
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

type dvLatencyMeasurementFactory struct {
	baseLatencyMeasurementFactory
}

func newDvLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]interface{}) (measurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedDvConditions); err != nil {
		return nil, err
	}
	return dvLatencyMeasurementFactory{
		baseLatencyMeasurementFactory: newBaseLatencyMeasurementFactory(configSpec, measurement, metadata),
	}, nil
}

func (dvlmf dvLatencyMeasurementFactory) newMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config) measurement {
	return &dvLatency{
		baseLatencyMeasurement: dvlmf.newBaseLatency(jobConfig, clientSet, restConfig),
	}
}

func getCDIClient(restConfig *rest.Config) *rest.RESTClient {
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)

	// Add CDI objects to the scheme
	metav1.AddToGroupVersion(scheme, metav1.SchemeGroupVersion)
	scheme.AddKnownTypes(cdiv1beta1.SchemeGroupVersion, &cdiv1beta1.DataVolumeList{}, &cdiv1beta1.DataVolume{})

	shallowCopy := *restConfig
	shallowCopy.ContentConfig.GroupVersion = &cdiv1beta1.SchemeGroupVersion

	shallowCopy.APIPath = "/apis"
	shallowCopy.NegotiatedSerializer = codecs.WithoutConversion()
	shallowCopy.UserAgent = rest.DefaultKubernetesUserAgent()

	cdiClient, err := rest.RESTClientFor(&shallowCopy)
	if err != nil {
		log.Fatalf("failed to create CDI Client - %v", err)
	}
	return cdiClient
}

func (dv *dvLatency) handleCreateDV(obj interface{}) {
	dataVolume := obj.(*cdiv1beta1.DataVolume)
	dvLabels := dataVolume.GetLabels()
	dv.metrics.LoadOrStore(string(dataVolume.UID), dvMetric{
		Timestamp:    dataVolume.CreationTimestamp.Time.UTC(),
		Namespace:    dataVolume.Namespace,
		Name:         dataVolume.Name,
		MetricName:   dvLatencyMeasurement,
		UUID:         dv.uuid,
		JobName:      dv.jobConfig.Name,
		Metadata:     dv.metadata,
		JobIteration: getIntFromLabels(dvLabels, config.KubeBurnerLabelJobIteration),
		Replica:      getIntFromLabels(dvLabels, config.KubeBurnerLabelReplica),
	})
}

func (dv *dvLatency) handleUpdateDV(obj interface{}) {
	dataVolume := obj.(*cdiv1beta1.DataVolume)
	if value, exists := dv.metrics.Load(string(dataVolume.UID)); exists {
		dvm := value.(dvMetric)
		for _, c := range dataVolume.Status.Conditions {
			// Nothing to update if the condition is not true
			if c.Status != corev1.ConditionTrue {
				continue
			}
			switch c.Type {
			case cdiv1beta1.DataVolumeBound:
				if dvm.dvBound.IsZero() {
					log.Debugf("Updated bound time for dataVolume [%s]", dataVolume.Name)
					dvm.dvBound = c.LastTransitionTime.Time.UTC()
				}
			case cdiv1beta1.DataVolumeRunning:
				if dvm.dvRunning.IsZero() {
					log.Debugf("Updated running time for dataVolume [%s]", dataVolume.Name)
					dvm.dvRunning = c.LastTransitionTime.Time.UTC()
				}
			case cdiv1beta1.DataVolumeReady:
				if dvm.dvReady.IsZero() {
					log.Infof("Updated ready time for dataVolume [%s]", dataVolume.Name)
					dvm.dvReady = c.LastTransitionTime.Time.UTC()
				}
			}
		}
		dv.metrics.Store(string(dataVolume.UID), dvm)
	}
}

func (dv *dvLatency) start(measurementWg *sync.WaitGroup) error {
	defer measurementWg.Done()
	// Reset latency slices, required in multi-job benchmarks
	dv.latencyQuantiles, dv.normLatencies = nil, nil
	dv.metrics = sync.Map{}
	log.Infof("Creating Data Volume latency watcher for %s", dv.jobConfig.Name)
	dv.watcher = metrics.NewWatcher(
		getCDIClient(dv.restConfig),
		"dvWatcher",
		"datavolumes",
		corev1.NamespaceAll,
		func(options *metav1.ListOptions) {
			options.LabelSelector = fmt.Sprintf("kube-burner-runid=%v", dv.runid)
		},
		nil,
	)
	dv.watcher.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: dv.handleCreateDV,
		UpdateFunc: func(oldObj, newObj interface{}) {
			dv.handleUpdateDV(newObj)
		},
	})
	if err := dv.watcher.StartAndCacheSync(); err != nil {
		log.Errorf("DataVolume Latency measurement error: %s", err)
	}

	return nil
}

func (dv *dvLatency) stop() error {
	var err error
	defer func() {
		if dv.watcher != nil {
			dv.watcher.StopWatcher()
		}
	}()
	errorRate := dv.normalizeMetrics()
	if errorRate > 10.00 {
		log.Error("Latency errors beyond 10%. Hence invalidating the results")
		return fmt.Errorf("something is wrong with system under test. DataVolume latencies error rate was: %.2f", errorRate)
	}
	dv.calcQuantiles()
	if len(dv.config.LatencyThresholds) > 0 {
		err = metrics.CheckThreshold(dv.config.LatencyThresholds, dv.latencyQuantiles)
	}
	for _, q := range dv.latencyQuantiles {
		pq := q.(metrics.LatencyQuantiles)
		log.Infof("%s: %v 99th: %v max: %v avg: %v", dv.jobConfig.Name, pq.QuantileName, pq.P99, pq.Max, pq.Avg)
	}
	if errorRate > 0 {
		log.Infof("DV latencies error rate was: %.2f", errorRate)
	}
	return err
}

func (dv *dvLatency) collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var dataVolumes []cdiv1beta1.DataVolume
	labelSelector := labels.SelectorFromSet(dv.jobConfig.NamespaceLabels)
	options := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	kubeVirtClient, err := kubecli.GetKubevirtClientFromRESTConfig(dv.restConfig)
	if err != nil {
		log.Fatalf("Failed to get kubevirt client - %v", err)
	}
	namespaces := strings.Split(dv.jobConfig.Namespace, ",")
	for _, namespace := range namespaces {
		dvList, err := kubeVirtClient.CdiClient().CdiV1beta1().DataVolumes(namespace).List(context.TODO(), options)
		if err != nil {
			log.Errorf("error listing dataVolumes in namespace %s: %v", namespace, err)
		}
		dataVolumes = append(dataVolumes, dvList.Items...)
	}
	dv.metrics = sync.Map{}
	for _, dataVolume := range dataVolumes {
		var bound, running, ready time.Time
		for _, c := range dataVolume.Status.Conditions {
			switch c.Type {
			case cdiv1beta1.DataVolumeBound:
				bound = c.LastTransitionTime.Time.UTC()
			case cdiv1beta1.DataVolumeRunning:
				running = c.LastTransitionTime.Time.UTC()
			case cdiv1beta1.DataVolumeReady:
				ready = c.LastTransitionTime.Time.UTC()
			}
		}
		dv.metrics.Store(string(dataVolume.UID), dvMetric{
			Timestamp:  dataVolume.CreationTimestamp.Time.UTC(),
			Namespace:  dataVolume.Namespace,
			Name:       dataVolume.Name,
			MetricName: dvLatencyMeasurement,
			UUID:       dv.uuid,
			dvBound:    bound,
			dvRunning:  running,
			dvReady:    ready,
			JobName:    dv.jobConfig.Name,
		})
	}
}

func (dv *dvLatency) index(jobName string, indexerList map[string]indexers.Indexer) {
	metricMap := map[string][]interface{}{
		dvLatencyMeasurement:          dv.normLatencies,
		dvLatencyQuantilesMeasurement: dv.latencyQuantiles,
	}
	IndexLatencyMeasurement(dv.config, jobName, metricMap, indexerList)
}

func (dv *dvLatency) getMetrics() *sync.Map {
	return &dv.metrics
}

func (dv *dvLatency) normalizeMetrics() float64 {
	dataVolumeCount := 0
	erroredDataVolumes := 0

	dv.metrics.Range(func(key, value interface{}) bool {
		m := value.(dvMetric)
		// Skip DataVolume if it did not reach the Ready state (this timestamp isn't set)
		if m.dvReady.IsZero() {
			log.Warningf("DataVolume %v latency ignored as it did not reach Ready state", m.Name)
			return true
		}
		// latencyTime should be always larger than zero, however, in some cases, it might be a
		// negative value due to the precision of timestamp can only get to the level of second
		// and also the creation timestamp we capture using time.Now().UTC() might even have a
		// delay over 1s in some cases. The microsecond and nanosecond have been discarded purposely
		// in kubelet, this is because apiserver does not support RFC339NANO. The newly introduced
		// v2 latencies are currently under AB testing which blindly trust kubernetes as source of
		// truth and will prevent us from those over 1s delays as well as <0 cases.
		errorFlag := 0
		m.DVBoundLatency = int(m.dvBound.Sub(m.Timestamp).Milliseconds())
		if m.DVBoundLatency < 0 {
			log.Tracef("DVBoundLatency for DataVolume %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.DVBoundLatency = 0
		}

		m.DVRunningLatency = int(m.dvRunning.Sub(m.Timestamp).Milliseconds())
		if m.DVRunningLatency < 0 {
			log.Tracef("DVRunningLatency for DataVolume %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.DVBoundLatency = 0
		}

		m.DVReadyLatency = int(m.dvReady.Sub(m.Timestamp).Milliseconds())
		if m.DVReadyLatency < 0 {
			log.Tracef("DVReadyLatency for DataVolume %v falling under negative case. So explicitly setting it to 0", m.Name)
			errorFlag = 1
			m.DVReadyLatency = 0
		}
		dataVolumeCount++
		erroredDataVolumes += errorFlag
		dv.normLatencies = append(dv.normLatencies, m)
		return true
	})
	if dataVolumeCount == 0 {
		return 0.0
	}
	return float64(erroredDataVolumes) / float64(dataVolumeCount) * 100.0
}

func (dv *dvLatency) calcQuantiles() {
	getLatency := func(normLatency interface{}) map[string]float64 {
		dataVolumeMetric := normLatency.(dvMetric)
		return map[string]float64{
			string(cdiv1beta1.DataVolumeBound):   float64(dataVolumeMetric.DVBoundLatency),
			string(cdiv1beta1.DataVolumeRunning): float64(dataVolumeMetric.DVRunningLatency),
			string(cdiv1beta1.DataVolumeReady):   float64(dataVolumeMetric.DVReadyLatency),
		}
	}
	dv.latencyQuantiles = calculateQuantiles(dv.uuid, dv.jobConfig.Name, dv.metadata, dv.normLatencies, getLatency, dvLatencyQuantilesMeasurement)
}
