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

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/config"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type podMetric struct {
	Timestamp              time.Time `json:"timestamp"`
	scheduled              time.Time
	SchedulingLatency      int64 `json:"schedulingLatency"`
	initialized            time.Time
	InitializedLatency     int64 `json:"initializedLatency"`
	containersReady        time.Time
	ContainersReadyLatency int64 `json:"containersReadyLatency"`
	podReady               time.Time
	PodReadyLatency        int64  `json:"podReadyLatency"`
	MetricName             string `json:"metricName"`
	UUID                   string `json:"uuid"`
}

type podLatencyQuantiles struct {
	QuantileName string    `json:"quantileName"`
	UUID         string    `json:"uuid"`
	P99          int       `json:"P99"`
	P95          int       `json:"P95"`
	P50          int       `json:"P50"`
	Max          int       `json:"max"`
	Avg          float64   `json:"avg"`
	Timestamp    time.Time `json:"timestamp"`
	MetricName   string    `json:"metricName"`
}

var podQuantiles []interface{}
var normLatencies []interface{}
var podMetrics map[string]podMetric

const (
	informerTimeout = time.Minute
)

type podLatency struct {
	informer    cache.SharedInformer
	stopChannel chan struct{}
	config      config.Measurement
}

func init() {
	measurementMap["podLatency"] = &podLatency{}
}

func (p *podLatency) createPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if _, exists := podMetrics[string(pod.UID)]; !exists {
		if strings.Contains(pod.Namespace, factory.config.Namespace) {
			podMetrics[string(pod.UID)] = podMetric{
				Timestamp:  time.Now(),
				MetricName: "podLatencyMeasurement",
				UUID:       factory.uuid,
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

func (p *podLatency) setConfig(cfg config.Measurement) {
	p.config = cfg
}

// Start starts podLatency measurement
func (p *podLatency) start() {
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
		if err != nil {
			return fmt.Errorf("Error creating pod-latency metrics file %s: %s", filename, err)
		}
		defer f.Close()
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
func (p *podLatency) stop() error {
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
	if factory.globalConfig.WriteToFile {
		if err := p.writeToFile(); err != nil {
			log.Errorf("Error writing measurement podLatency: %s", err)
		}
	}
	if factory.globalConfig.IndexerConfig.Enabled {
		p.index()
	}
	return nil
}

// index sends metrics to the configured indexer
func (p *podLatency) index() {
	(*factory.indexer).Index(p.config.ESIndex, normLatencies)
	(*factory.indexer).Index(p.config.ESIndex, podQuantiles)
}

func normalizeMetrics() {
	for _, m := range podMetrics {
		m.SchedulingLatency = m.scheduled.Sub(m.Timestamp).Milliseconds()
		m.ContainersReadyLatency = m.containersReady.Sub(m.Timestamp).Milliseconds()
		m.InitializedLatency = m.initialized.Sub(m.Timestamp).Milliseconds()
		m.PodReadyLatency = m.podReady.Sub(m.Timestamp).Milliseconds()
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
		quantileMap["scheduling"] = append(quantileMap["scheduling"], int(l.(podMetric).SchedulingLatency))
		quantileMap["containersReady"] = append(quantileMap["containersReady"], int(l.(podMetric).ContainersReadyLatency))
		quantileMap["initialized"] = append(quantileMap["initialized"], int(l.(podMetric).InitializedLatency))
		quantileMap["podReady"] = append(quantileMap["podReady"], int(l.(podMetric).PodReadyLatency))
	}
	for quantileName, v := range quantileMap {
		podQ := podLatencyQuantiles{
			QuantileName: quantileName,
			UUID:         factory.uuid,
			Timestamp:    time.Now(),
			MetricName:   "podLatencyQuantilesMeasurement",
		}
		sort.Ints(v)
		length := len(v)
		for _, quantile := range quantiles {
			qValue := v[int(math.Ceil(float64(length)*quantile))-1]
			podQ.setQuantile(quantile, qValue)
		}
		podQ.Max = v[length-1]
		sum := 0
		for _, n := range v {
			sum += n
		}
		podQ.Avg = math.Round(100*float64(sum)/float64(length)) / 100
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
