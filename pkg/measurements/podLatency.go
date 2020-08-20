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

package measurements

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/rsevilla87/kube-burner/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type podMetric struct {
	Namespace         string    `json:"namespace"`
	UUID              string    `json:"uuid"`
	Name              string    `json:"podName"`
	JobName           string    `json:"jobName"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
	RunningLatency    int64     `json:"runningLatency"`
	ScheduledLatency  float64   `json:"schedulingLatency"`
}

var podMetrics []podMetric

const (
	informerTimeout = time.Minute
)

type podLatency struct {
	indexName   string
	uuid        string
	informer    cache.SharedInformer
	stopChannel chan struct{}
}

func init() {
	measurementMap["podLatency"] = &podLatency{}
}

func (p *podLatency) updatePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if strings.Contains(pod.Namespace, factory.config.Namespace) {
		if pod.Status.Phase == v1.PodRunning {
			pm := podMetric{
				Namespace:         pod.Namespace,
				Name:              pod.Name,
				JobName:           factory.config.Name,
				UUID:              factory.uuid,
				CreationTimestamp: pod.CreationTimestamp.Time,
			}
			for _, c := range pod.Status.Conditions {
				switch c.Type {
				case v1.PodScheduled:
					pm.ScheduledLatency = c.LastTransitionTime.Time.Sub(pod.CreationTimestamp.Time).Seconds()
				case v1.PodReady:
					pm.RunningLatency = time.Since(pod.CreationTimestamp.Time).Milliseconds()
				}
			}
			podMetrics = append(podMetrics, pm)
		}
	}
}

var ready = make(chan bool)

// Start starts podLatency measurement
func (p *podLatency) Start(factory measurementFactory) {
	podMetrics = []podMetric{}
	log.Infof("Creating Pod latency informer for %s", factory.config.Name)
	podListWatcher := cache.NewFilteredListWatchFromClient(factory.clientSet.CoreV1().RESTClient(), "pods", v1.NamespaceAll, func(options *metav1.ListOptions) {})
	p.informer = cache.NewSharedInformer(podListWatcher, nil, 0)
	p.stopChannel = make(chan struct{})
	p.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.updatePod(newObj)
		},
	})
	if err := p.startAndSync(); err != nil {
		log.Errorf("Pod Latency measurement error: %s", err)
	}
	ready <- true
}

func (p *podLatency) SetMetadata(indexName string) {
	p.indexName = indexName
}

func (p *podLatency) Index() {
	podMetricsinterface := make([]interface{}, len(podMetrics))
	for _, metric := range podMetrics {
		metric.UUID = p.uuid
		podMetricsinterface = append(podMetricsinterface, metric)
	}
	(*factory.indexer).Index(p.indexName, podMetricsinterface)
}

func (p *podLatency) waitForReady() {
	<-ready
}

func (p *podLatency) writeToFile() error {
	filename := fmt.Sprintf("%s-podLatency.json", factory.config.Name)
	if factory.globalConfig.MetricsDirectory != "" {
		err := os.MkdirAll(factory.globalConfig.MetricsDirectory, 0744)
		if err != nil {
			return fmt.Errorf("Error creating metrics directory %s: ", err)
		}
		filename = path.Join(factory.globalConfig.MetricsDirectory, filename)
	}
	f, err := os.Create(filename)
	defer f.Close()
	if err != nil {
		return fmt.Errorf("Error creating pod-latency metrics file %s: %s", filename, err)
	}
	jsonEnc := json.NewEncoder(f)
	jsonEnc.SetIndent("", "  ")
	log.Infof("Writing pod latency metrics in %s", filename)
	if err := jsonEnc.Encode(podMetrics); err != nil {
		return fmt.Errorf("JSON encoding error: %s", err)
	}
	return nil
}

// StartAndSync starts informer and waits for it to be synced.
func (p *podLatency) startAndSync() error {
	go p.informer.Run(p.stopChannel)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerTimeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, p.informer.HasSynced) {
		return fmt.Errorf("Pod-latency: Timed out waiting for caches to sync")
	}
	return nil
}

func (p *podLatency) Stop() error {
	defer close(p.stopChannel)
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerTimeout, func() {
		close(timeoutCh)
	})
	defer timeoutTimer.Stop()
	if !cache.WaitForCacheSync(timeoutCh, p.informer.HasSynced) {
		return fmt.Errorf("Pod-latency: Timed out waiting for caches to sync")
	}
	return nil
}
