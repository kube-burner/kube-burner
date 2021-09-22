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
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type podMetric struct {
	Timestamp              time.Time `json:"timestamp"`
	scheduled              time.Time
	SchedulingLatency      int `json:"schedulingLatency"`
	initialized            time.Time
	InitializedLatency     int `json:"initializedLatency"`
	containersReady        time.Time
	ContainersReadyLatency int `json:"containersReadyLatency"`
	podReady               time.Time
	PodReadyLatency        int    `json:"podReadyLatency"`
	MetricName             string `json:"metricName"`
	JobName                string `json:"jobName"`
	UUID                   string `json:"uuid"`
	Namespace              string `json:"namespace"`
	Name                   string `json:"podName"`
	NodeName               string `json:"nodeName"`
}

type podLatencyQuantiles struct {
	QuantileName v1.PodConditionType `json:"quantileName"`
	UUID         string              `json:"uuid"`
	P99          int                 `json:"P99"`
	P95          int                 `json:"P95"`
	P50          int                 `json:"P50"`
	Max          int                 `json:"max"`
	Avg          int                 `json:"avg"`
	Timestamp    time.Time           `json:"timestamp"`
	MetricName   string              `json:"metricName"`
	JobName      string              `json:"jobName"`
}

var podQuantiles []interface{}
var normLatencies []interface{}
var podMetrics map[string]podMetric

const (
	informerTimeout                = time.Minute
	podLatencyMeasurement          = "podLatencyMeasurement"
	podLatencyQuantilesMeasurement = "podLatencyQuantilesMeasurement"
)

type podLatency struct {
	informer    cache.SharedInformer
	stopChannel chan struct{}
	config      types.Measurement
}

func init() {
	measurementMap["podLatency"] = &podLatency{}
}

func (p *podLatency) createPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if _, exists := podMetrics[string(pod.UID)]; !exists {
		if strings.Contains(pod.Namespace, factory.jobConfig.Namespace) {
			podMetrics[string(pod.UID)] = podMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  pod.Namespace,
				Name:       pod.Name,
				MetricName: podLatencyMeasurement,
				UUID:       factory.uuid,
				JobName:    factory.jobConfig.Name,
			}
		}
	}
}

func (p *podLatency) updatePod(obj interface{}) {
	pod := obj.(*v1.Pod)
	if pm, exists := podMetrics[string(pod.UID)]; exists && pm.podReady.IsZero() {
		for _, c := range pod.Status.Conditions {
			if c.Status == v1.ConditionTrue {
				switch c.Type {
				case v1.PodScheduled:
					if pm.scheduled.IsZero() {
						pm.scheduled = time.Now().UTC()
						pm.NodeName = pod.Spec.NodeName
					}
				case v1.PodInitialized:
					if pm.initialized.IsZero() {
						pm.initialized = time.Now().UTC()
					}
				case v1.ContainersReady:
					if pm.containersReady.IsZero() {
						pm.containersReady = time.Now().UTC()
					}
				case v1.PodReady:
					log.Debugf("Pod %s is ready", pod.Name)
					pm.podReady = time.Now().UTC()
				}
			}
		}
		podMetrics[string(pod.UID)] = pm
	}
}

func (p *podLatency) setConfig(cfg types.Measurement) error {
	p.config = cfg
	if err := p.validateConfig(); err != nil {
		return err
	}
	return nil
}

// Start starts podLatency measurement
func (p *podLatency) start() {
	podMetrics = make(map[string]podMetric)
	log.Infof("Creating Pod latency informer for %s", factory.jobConfig.Name)
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
		fmt.Sprintf("%s-podLatency.json", factory.jobConfig.Name):         normLatencies,
		fmt.Sprintf("%s-podLatency-summary.json", factory.jobConfig.Name): podQuantiles,
	}
	for filename, data := range filesMetrics {
		if kubeburnerCfg.MetricsDirectory != "" {
			err := os.MkdirAll(kubeburnerCfg.MetricsDirectory, 0744)
			if err != nil {
				return fmt.Errorf("Error creating metrics directory: %s", err)
			}
			filename = path.Join(kubeburnerCfg.MetricsDirectory, filename)
		}
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("Error creating pod-latency metrics file %s: %s", filename, err)
		}
		defer f.Close()
		jsonEnc := json.NewEncoder(f)
		jsonEnc.SetIndent("", "  ")
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
func (p *podLatency) stop() (int, error) {
	timeoutCh := make(chan struct{})
	timeoutTimer := time.AfterFunc(informerTimeout, func() {
		close(timeoutCh)
	})
	if !cache.WaitForCacheSync(timeoutCh, p.informer.HasSynced) {
		return 0, fmt.Errorf("Pod-latency: Timed out waiting for caches to sync")
	}
	timeoutTimer.Stop()
	close(p.stopChannel)
	normalizeMetrics()
	calcQuantiles()
	rc := p.checkThreshold()
	if kubeburnerCfg.WriteToFile {
		if err := p.writeToFile(); err != nil {
			log.Errorf("Error writing measurement podLatency: %s", err)
		}
	}
	if kubeburnerCfg.IndexerConfig.Enabled {
		p.index()
	}
	return rc, nil
}

// index sends metrics to the configured indexer
func (p *podLatency) index() {
	(*factory.indexer).Index(p.config.ESIndex, normLatencies)
	(*factory.indexer).Index(p.config.ESIndex, podQuantiles)
}

func normalizeMetrics() {
	for _, m := range podMetrics {
		// If a does not reach the Running state (this timestamp wasn't set), we skip that pod
		if m.podReady.IsZero() {
			continue
		}
		m.SchedulingLatency = int(m.scheduled.Sub(m.Timestamp).Milliseconds())
		m.ContainersReadyLatency = int(m.containersReady.Sub(m.Timestamp).Milliseconds())
		m.InitializedLatency = int(m.initialized.Sub(m.Timestamp).Milliseconds())
		m.PodReadyLatency = int(m.podReady.Sub(m.Timestamp).Milliseconds())
		normLatencies = append(normLatencies, m)
	}
}

func calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[v1.PodConditionType][]int{}
	for _, normLatency := range normLatencies {
		quantileMap[v1.PodScheduled] = append(quantileMap[v1.PodScheduled], normLatency.(podMetric).SchedulingLatency)
		quantileMap[v1.ContainersReady] = append(quantileMap[v1.ContainersReady], normLatency.(podMetric).ContainersReadyLatency)
		quantileMap[v1.PodInitialized] = append(quantileMap[v1.PodInitialized], normLatency.(podMetric).InitializedLatency)
		quantileMap[v1.PodReady] = append(quantileMap[v1.PodReady], normLatency.(podMetric).PodReadyLatency)
	}
	for quantileName, v := range quantileMap {
		podQ := podLatencyQuantiles{
			QuantileName: quantileName,
			UUID:         factory.uuid,
			Timestamp:    time.Now().UTC(),
			JobName:      factory.jobConfig.Name,
			MetricName:   podLatencyQuantilesMeasurement,
		}
		sort.Ints(v)
		length := len(v)
		if length > 1 {
			for _, quantile := range quantiles {
				qValue := v[int(math.Ceil(float64(length)*quantile))-1]
				podQ.setQuantile(quantile, qValue)
			}
			podQ.Max = v[length-1]
		}
		sum := 0
		for _, n := range v {
			sum += n
		}
		podQ.Avg = int(math.Round(float64(sum) / float64(length)))
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

func (p *podLatency) checkThreshold() int {
	var rc int
	log.Info("Evaluating latency thresholds")
	for _, phase := range p.config.LatencyThresholds {
		for _, pq := range podQuantiles {
			if phase.ConditionType == pq.(podLatencyQuantiles).QuantileName {
				// Required to acccess the attribute by name
				r := reflect.ValueOf(pq.(podLatencyQuantiles))
				v := r.FieldByName(phase.Metric).Int()
				if v > phase.Threshold.Milliseconds() {
					log.Warnf("%s %s latency (%dms) higher than configured threshold: %v", phase.Metric, phase.ConditionType, v, phase.Threshold)
					rc = 1
				} else {
					log.Infof("%s %s latency (%dms) meets the configured threshold: %v", phase.Metric, phase.ConditionType, v, phase.Threshold)
				}
				continue
			}
		}
	}
	return rc
}

func (p *podLatency) validateConfig() error {
	var metricFound bool
	var latencyMetrics []string = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range p.config.LatencyThresholds {
		if th.ConditionType == v1.ContainersReady || th.ConditionType == v1.PodInitialized || th.ConditionType == v1.PodReady || th.ConditionType == v1.PodScheduled {
			for _, lm := range latencyMetrics {
				if th.Metric == lm {
					metricFound = true
					break
				}
			}
			if !metricFound {
				return fmt.Errorf("Unsupported metric %s in podLatency measurement, supported are: %s", th.Metric, strings.Join(latencyMetrics, ", "))
			}
		} else {
			return fmt.Errorf("Unsupported pod condition type in podLatency measurement: %s", th.ConditionType)
		}
	}
	return nil
}
