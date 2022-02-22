// Copyright 2021 The Kube-burner Authors.
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

package watcher

import (
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	btypes "github.com/cloud-bulldozer/kube-burner/pkg/burner/types"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/metrics"
	mtypes "github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	k8sv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

func (p *Watcher) handleCreatePod(obj interface{}) {
	pod := obj.(*k8sv1.Pod)
	podID := getPodID(*pod)
	if _, exists := p.GetMetric(string(podID)); !exists {
		if strings.Contains(pod.Namespace, jobNamespace) {
			podM := metrics.PodMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  pod.Namespace,
				Name:       pod.Name,
				MetricName: jobMetricName,
				UUID:       jobUUID,
				JobName:    jobName,
			}
			p.AddMetric(string(podID), &podM)
		}
	}
	m, _ := p.GetMetric(string(podID))
	podM := m.(*metrics.PodMetric)
	if podM.PodCreated == nil {
		t := time.Now().UTC()
		podM.PodCreated = &t
		p.AddMetric(string(podID), podM)
	}
}

func (p *Watcher) handleUpdatePod(obj interface{}) {
	pod := obj.(*k8sv1.Pod)
	podID := getPodID(*pod)
	if pm, exists := p.GetMetric(podID); exists && pm != nil {
		if pm.(*metrics.PodMetric).PodReady == nil {
			podM := pm.(*metrics.PodMetric)
			for _, c := range pod.Status.Conditions {
				if c.Status == v1.ConditionTrue {
					switch c.Type {
					case v1.PodScheduled:
						if podM.PodScheduled == nil {
							t := time.Now().UTC()
							podM.PodScheduled = &t
							podM.NodeName = pod.Spec.NodeName
						}
					case v1.PodInitialized:
						if podM.PodInitialized == nil {
							t := time.Now().UTC()
							podM.PodInitialized = &t
						}
					case v1.ContainersReady:
						if podM.PodContainersReady == nil {
							t := time.Now().UTC()
							podM.PodContainersReady = &t
						}
					case v1.PodReady:
						log.Debugf("Pod %s is ready", pod.Name)
						t := time.Now().UTC()
						podM.PodReady = &t
						p.AddResourceStatePerNS(btypes.PodResource, "Ready", pod.Namespace, 1)
					}
				}
			}
			p.AddMetric(string(pod.UID), podM)
		}
	}
}

func NewPodWatcher(restClient *rest.RESTClient, namespace string, resourceMetricName string, uuid string, name string) *Watcher {
	jobNamespace = namespace
	jobMetricName = resourceMetricName
	jobUUID = uuid
	jobName = name
	w := NewWatcher(
		restClient,
		mtypes.PodWatcher,
		btypes.PodResource,
		namespace,
	)
	w.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: w.handleCreatePod,
		UpdateFunc: func(oldObj, newObj interface{}) {
			w.handleUpdatePod(newObj)
		},
	})
	if err := w.StartAndCacheSync(); err != nil {
		log.Errorf("Pod watcher start and cache sync error: %s", err)
	}
	return w
}
