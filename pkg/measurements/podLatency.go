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
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/rsevilla87/kube-burner/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type podMetric struct {
	Namespace              string    `json:"namespace"`
	UUID                   string    `json:"uuid"`
	Name                   string    `json:"podName"`
	JobName                string    `json:"jobName"`
	Creation               time.Time `json:"created"`
	scheduled              time.Time
	SchedulingLatency      int64 `json:"schedulingLatency"`
	initialized            time.Time
	InitializedLatency     int64 `json:"initializedLatency"`
	containersReady        time.Time
	ContainersReadyLatency int64 `json:"containersReadyLatency"`
	podReady               time.Time
	PodReadyLatency        int64 `json:"podReadyLatency"`
}
type podLatencyQuantiles struct {
	Name    string `json:"quantileName"`
	JobName string `json:"jobName"`
	UUID    string `json:"uuid"`
	P99     int    `json:"P99"`
	P95     int    `json:"P95"`
	P50     int    `json:"P50"`
}

var podQuantiles []podLatencyQuantiles
var normLatencies []podMetric
var podMetrics map[string]podMetric

const (
	informerTimeout = time.Minute
)

type podLatency struct {
	indexName   string
	informer    cache.SharedInformer
	stopChannel chan struct{}
}

func init() {
	measurementMap["podLatency"] = &podLatency{}
}

func (p *podLatency) createPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if _, exists := podMetrics[string(pod.UID)]; !exists {
		if strings.Contains(pod.Namespace, factory.config.Namespace) {
			podMetrics[string(pod.UID)] = podMetric{
				Namespace: pod.Namespace,
				Name:      pod.Name,
				JobName:   factory.config.Name,
				UUID:      factory.uuid,
				Creation:  time.Now(),
			}
		}
	}
}

func (p *podLatency) updatePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if pm, exists := podMetrics[string(pod.UID)]; exists {
		for _, c := range pod.Status.Conditions {
			switch c.Type {
			case v1.PodScheduled:
				if pm.scheduled.IsZero() {
					pm.scheduled = time.Now()
				}
			case v1.PodInitialized:
				if pm.initialized.IsZero() {
					pm.initialized = time.Now()
				}
			case v1.ContainersReady:
				if pm.containersReady.IsZero() {
					pm.containersReady = time.Now()
				}
			case v1.PodReady:
				if pm.podReady.IsZero() {
					pm.podReady = time.Now()
				}
			}
		}
		podMetrics[string(pod.UID)] = pm
	}
}

var ready = make(chan bool)

// Start starts podLatency measurement
func (p *podLatency) Start(factory measurementFactory) {
	podMetrics = make(map[string]podMetric)
	log.Infof("Creating Pod latency informer for %s", factory.config.Name)
	podListWatcher := cache.NewFilteredListWatchFromClient(factory.clientSet.CoreV1().RESTClient(), "pods", v1.NamespaceAll, func(options *metav1.ListOptions) {})
	p.informer = cache.NewSharedInformer(podListWatcher, nil, 0)
	p.stopChannel = make(chan struct{})
	p.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: p.createPod,
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

// Index sends metrics to the configured indexer
func (p *podLatency) Index() {
	podMetricsinterface := make([]interface{}, len(normLatencies))
	for _, podLatency := range normLatencies {
		podMetricsinterface = append(podMetricsinterface, podLatency)
	}
	(*factory.indexer).Index(p.indexName, podMetricsinterface)
	podQuantilesinterface := make([]interface{}, len(podQuantiles))
	for _, podQuantile := range podQuantiles {
		podQuantilesinterface = append(podQuantilesinterface, podQuantile)
	}
	indexName := fmt.Sprintf("%s-summary", p.indexName)
	(*factory.indexer).Index(indexName, podQuantilesinterface)
}

func (p *podLatency) waitForReady() {
	<-ready
}

func (p *podLatency) writeToFile() error {
	filesMetrics := map[string]interface{}{
		fmt.Sprintf("%s-podLatency.json", factory.config.Name):         normLatencies,
		fmt.Sprintf("%s-podLatency-summary.json", factory.config.Name): podQuantiles,
	}
	for filename, data := range filesMetrics {
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
		jsonEnc.SetIndent("", "    ")
		log.Infof("Writing pod latency metrics in %s", filename)
		if err := jsonEnc.Encode(data); err != nil {
			return fmt.Errorf("JSON encoding error: %s", err)
		}
	}
	return nil
}

// startAndSync starts informer and waits for it to be synced.
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

// Stop stops podLatency measurement
func (p *podLatency) Stop() error {
	normalizeMetrics()
	calcQuantiles()
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

func normalizeMetrics() {
	for _, m := range podMetrics {
		m.SchedulingLatency = m.scheduled.Sub(m.Creation).Milliseconds()
		m.ContainersReadyLatency = m.containersReady.Sub(m.Creation).Milliseconds()
		m.InitializedLatency = m.initialized.Sub(m.Creation).Milliseconds()
		m.PodReadyLatency = m.podReady.Sub(m.Creation).Milliseconds()
		normLatencies = append(normLatencies, m)
	}
}

func calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[string][]int{
		"scheduling":      {},
		"containersReady": {},
		"initialized":     {},
		"podReady":        {},
	}
	for _, l := range normLatencies {
		quantileMap["scheduling"] = append(quantileMap["scheduling"], int(l.SchedulingLatency))
		quantileMap["containersReady"] = append(quantileMap["containersReady"], int(l.ContainersReadyLatency))
		quantileMap["initialized"] = append(quantileMap["initialized"], int(l.InitializedLatency))
		quantileMap["podReady"] = append(quantileMap["podReady"], int(l.PodReadyLatency))
	}
	for k, v := range quantileMap {
		podQ := podLatencyQuantiles{
			Name:    k,
			UUID:    factory.uuid,
			JobName: factory.config.Name,
		}
		sort.Ints(v)
		length := len(v)
		for _, quantile := range quantiles {
			qValue := v[int(math.Ceil(float64(length)*quantile))]
			podQ.setQuantile(quantile, qValue)
		}
		podQuantiles = append(podQuantiles, podQ)
	}
}

func (plq *podLatencyQuantiles) setQuantile(quantile float64, qValue int) {
	switch quantile {
	case 0.5:
		plq.P50 = qValue
	case 0.95:
		plq.P95 = qValue
	case 0.99:
		plq.P99 = qValue
	}
}
