// Copyright 2024 The Kube-burner Authors.
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
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/v2/pkg/config"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/metrics"
	"github.com/kube-burner/kube-burner/v2/pkg/measurements/types"
	"github.com/kube-burner/kube-burner/v2/pkg/util"
	"github.com/kube-burner/kube-burner/v2/pkg/util/fileutils"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	nodeLatencyMeasurement          = "nodeLatencyMeasurement"
	nodeLatencyQuantilesMeasurement = "nodeLatencyQuantilesMeasurement"
)

var (
	supportedNodeConditions = map[string]struct{}{
		string(corev1.NodeMemoryPressure): {},
		string(corev1.NodeDiskPressure):   {},
		string(corev1.NodePIDPressure):    {},
		string(corev1.NodeReady):          {},
	}
)

type nodeLatencyLabels struct {
	Name string `json:"nodeName"`
}

type NodeMetric struct {
	metrics.LatencyDocument
	NodeMemoryPressure        time.Time `json:"-"`
	NodeMemoryPressureLatency int       `json:"nodeMemoryPressureLatency"`
	NodeDiskPressure          time.Time `json:"-"`
	NodeDiskPressureLatency   int       `json:"nodeDiskPressureLatency"`
	NodePIDPressure           time.Time `json:"-"`
	NodePIDPressureLatency    int       `json:"nodePIDPressureLatency"`
	NodeReady                 time.Time `json:"-"`
	NodeReadyLatency          int       `json:"nodeReadyLatency"`
	NodeLatencyLabels         nodeLatencyLabels
}

type nodeLatency struct {
	BaseMeasurement
}

type nodeLatencyMeasurementFactory struct {
	BaseMeasurementFactory
}

func newNodeLatencyMeasurementFactory(configSpec config.Spec, measurement types.Measurement, metadata map[string]any, labelSelector string) (MeasurementFactory, error) {
	if err := verifyMeasurementConfig(measurement, supportedNodeConditions); err != nil {
		return nil, err
	}
	return nodeLatencyMeasurementFactory{
		BaseMeasurementFactory: NewBaseMeasurementFactory(configSpec, measurement, metadata, labelSelector),
	}, nil
}

func (nlmf nodeLatencyMeasurementFactory) NewMeasurement(jobConfig *config.Job, clientSet kubernetes.Interface, restConfig *rest.Config, embedCfg *fileutils.EmbedConfiguration) Measurement {
	return &nodeLatency{
		BaseMeasurement: nlmf.NewBaseLatency(jobConfig, clientSet, restConfig, nodeLatencyMeasurement, nodeLatencyQuantilesMeasurement, embedCfg),
	}
}

func (n *nodeLatency) handleCreateNode(obj any) {
	node, err := util.ConvertAnyToTyped[corev1.Node](obj)
	if err != nil {
		log.Errorf("failed to convert to Node: %v", err)
		return
	}
	n.Metrics.LoadOrStore(string(node.UID), NodeMetric{
		NodeLatencyLabels: nodeLatencyLabels{
			Name: node.Name,
		},
		LatencyDocument: metrics.LatencyDocument{
			Timestamp:  node.CreationTimestamp.UTC(),
			MetricName: nodeLatencyMeasurement,
			UUID:       n.Uuid,
			JobName:    n.JobConfig.Name,
			Labels:     util.NormalizeLabels(node.Labels),
			Metadata:   n.Metadata,
		},
	})
}

func (n *nodeLatency) handleUpdateNode(obj any) {
	node, err := util.ConvertAnyToTyped[corev1.Node](obj)
	if err != nil {
		log.Errorf("failed to convert to Node: %v", err)
		return
	}
	if value, exists := n.Metrics.Load(string(node.UID)); exists {
		nm := value.(NodeMetric)
		for _, c := range node.Status.Conditions {
			switch c.Type {
			case corev1.NodeMemoryPressure:
				if c.Status == corev1.ConditionFalse {
					nm.NodeMemoryPressure = c.LastTransitionTime.UTC()
				}
			case corev1.NodeDiskPressure:
				if c.Status == corev1.ConditionFalse {
					nm.NodeDiskPressure = c.LastTransitionTime.UTC()
				}
			case corev1.NodePIDPressure:
				if c.Status == corev1.ConditionFalse {
					nm.NodePIDPressure = c.LastTransitionTime.UTC()
				}
			case corev1.NodeReady:
				if c.Status == corev1.ConditionTrue {
					log.Debugf("Node %s is ready", node.Name)
					nm.NodeReady = c.LastTransitionTime.UTC()
				}
			}
		}
		n.Metrics.Store(string(node.UID), nm)
	}
}

func (n *nodeLatency) Start(measurementWg *sync.WaitGroup) error {
	n.LatencyQuantiles, n.NormLatencies = nil, nil
	defer measurementWg.Done()
	var wg sync.WaitGroup
	wg.Add(1)
	n.Collect(&wg)
	wg.Wait()
	gvr, err := util.ResourceToGVR(n.RestConfig, "Node", "v1")
	if err != nil {
		return fmt.Errorf("error getting GVR for %s: %w", "Node", err)
	}
	n.startMeasurement(
		[]MeasurementWatcher{
			{
				dynamicClient: dynamic.NewForConfigOrDie(n.RestConfig),
				name:          "nodeWatcher",
				resource:      gvr,
				handlers: &cache.ResourceEventHandlerFuncs{
					AddFunc: n.handleCreateNode,
					UpdateFunc: func(oldObj, newObj any) {
						n.handleUpdateNode(newObj)
					},
				},
				transform: nodeTransformFunc(),
			},
		},
	)
	return nil
}

func (n *nodeLatency) Collect(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()
	var nodes []corev1.Node
	nodeList, err := n.ClientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Errorf("error listing nodes: %v", err)
	}
	nodes = append(nodes, nodeList.Items...)

	n.Metrics = sync.Map{}
	for _, node := range nodes {
		var nodeMemoryPressure, nodeDiskPressure, nodePIDPressure, nodeReady time.Time
		for _, c := range node.Status.Conditions {
			switch c.Type {
			case corev1.NodeMemoryPressure:
				nodeMemoryPressure = c.LastTransitionTime.UTC()
			case corev1.NodeDiskPressure:
				nodeDiskPressure = c.LastTransitionTime.UTC()
			case corev1.NodePIDPressure:
				nodePIDPressure = c.LastTransitionTime.UTC()
			case corev1.NodeReady:
				nodeReady = c.LastTransitionTime.UTC()
			}
		}
		n.Metrics.Store(string(node.UID), NodeMetric{
			LatencyDocument: metrics.LatencyDocument{
				Timestamp:  node.CreationTimestamp.UTC(),
				MetricName: nodeLatencyMeasurement,
				UUID:       n.Uuid,
				JobName:    n.JobConfig.Name,
				Labels:     util.NormalizeLabels(node.Labels),
			},
			NodeLatencyLabels: nodeLatencyLabels{
				Name: node.Name,
			},
			NodeMemoryPressure: nodeMemoryPressure,
			NodeDiskPressure:   nodeDiskPressure,
			NodePIDPressure:    nodePIDPressure,
			NodeReady:          nodeReady,
		})
	}
}

func (n *nodeLatency) Stop() error {
	return n.StopMeasurement(n.normalizeLatencies, n.getLatency)
}

func (n *nodeLatency) normalizeLatencies() float64 {
	n.Metrics.Range(func(key, value any) bool {
		m := value.(NodeMetric)
		// If a node does not reach the Ready state, we skip that node
		if m.NodeReady.IsZero() {
			log.Tracef("Node %v latency ignored as it did not reach Ready state", m.NodeLatencyLabels.Name)
			return true
		}
		earliest := m.Timestamp
		if m.NodeMemoryPressure.Before(earliest) {
			earliest = m.NodeMemoryPressure
		}
		if m.NodeDiskPressure.Before(earliest) {
			earliest = m.NodeDiskPressure
		}
		if m.NodePIDPressure.Before(earliest) {
			earliest = m.NodePIDPressure
		}
		m.NodeMemoryPressureLatency = int(m.NodeMemoryPressure.Sub(earliest).Milliseconds())
		m.NodeDiskPressureLatency = int(m.NodeDiskPressure.Sub(earliest).Milliseconds())
		m.NodePIDPressureLatency = int(m.NodePIDPressure.Sub(earliest).Milliseconds())
		m.NodeReadyLatency = int(m.NodeReady.Sub(earliest).Milliseconds())

		baseLabels := nodeLatencyLabels{
			Name: m.NodeLatencyLabels.Name,
		}
		makeDoc := func(condition string, valueMs int) metrics.LatencyDocument {
			var lbls map[string]string
			b, _ := json.Marshal(baseLabels)
			_ = json.Unmarshal(b, &lbls)
			lbls["condition"] = condition
			return metrics.LatencyDocument{
				Timestamp:  m.Timestamp,
				MetricName: nodeLatencyMeasurement,
				UUID:       n.Uuid,
				JobName:    n.JobConfig.Name,
				Metadata:   n.Metadata,
				Labels:     lbls,
				Value:      float64(valueMs),
			}
		}
		n.NormLatencies = append(n.NormLatencies,
			makeDoc(string(corev1.NodeMemoryPressure), m.NodeMemoryPressureLatency),
			makeDoc(string(corev1.NodeDiskPressure), m.NodeDiskPressureLatency),
			makeDoc(string(corev1.NodePIDPressure), m.NodePIDPressureLatency),
			makeDoc(string(corev1.NodeReady), m.NodeReadyLatency),
		)
		return true
	})
	return 0
}

func (n *nodeLatency) getLatency(normLatency any) map[string]float64 {
	nodeMetric := normLatency.(NodeMetric)
	condition := nodeMetric.Labels["condition"]
	return map[string]float64{condition: nodeMetric.Value}
}

func (n *nodeLatency) IsCompatible() bool {
	return true
}

// nodeTransformFunc preserves the following fields for latency measurements:
// - metadata: name, uid, creationTimestamp, labels
// - status: conditions
func nodeTransformFunc() cache.TransformFunc {
	return func(obj interface{}) (interface{}, error) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}

		// Nodes don't have namespace
		minimal := createMinimalUnstructured(u, metadataTransformOptions{includeLabels: true})

		if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
			_ = unstructured.SetNestedSlice(minimal.Object, conditions, "status", "conditions")
		}

		return minimal, nil
	}
}
