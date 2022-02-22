// Copyright 2022 The Kube-burner Authors.
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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloud-bulldozer/kube-burner/log"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/metrics"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/types"
	"github.com/cloud-bulldozer/kube-burner/pkg/measurements/watcher"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	kvv1 "kubevirt.io/api/core/v1"
)

var (
	latencyList = []string{
		"vm" + string(kvv1.VirtualMachineReady) + "Latency",
		"vmi" + string(kvv1.Pending) + "Latency",
		"vmi" + string(kvv1.Scheduling) + "Latency",
		"vmi" + string(kvv1.Scheduled) + "Latency",
		"vmi" + string(kvv1.VirtualMachineInstanceReady) + "Latency",
		"vmi" + "Unset" + "Latency",
		"vmi" + string(kvv1.VirtualMachineStatusProvisioning) + "Latency",
		"vmi" + string(kvv1.KubeVirtConditionSynchronized) + "Latency",
		"vmi" + string(kvv1.VirtualMachineInstanceAgentConnected) + "Latency",
		"vmi" + string(kvv1.VirtualMachineInstanceAccessCredentialsSynchronized) + "Latency",
		"vmi" + string(kvv1.Succeeded) + "Latency",
		"vmi" + string(kvv1.Failed) + "Latency",
		"vmi" + string(kvv1.Unknown),
		"pod" + "Created" + "Latency",
		"pod" + "Scheduled" + "Latency",
		"pod" + string(v1.PodInitialized) + "Latency",
		"pod" + string(v1.ContainersReady) + "Latency",
		"pod" + string(v1.PodReady) + "Latency"}
)

type vmLatency struct {
	config           types.Measurement
	watchers         map[string]*watcher.Watcher
	latencyQuantiles []interface{}
	normLatencies    []interface{}
}

func init() {
	MeasurementMap[types.VMLatency] = &vmLatency{watchers: make(map[string]*watcher.Watcher)}
}

func (p *vmLatency) SetConfig(cfg types.Measurement) error {
	p.config = cfg
	if err := p.validateConfig(); err != nil {
		return err
	}
	return nil
}

// Start starts vmLatency measurement
func (p *vmLatency) Start(measurementWg *sync.WaitGroup) {
	defer measurementWg.Done()

	log.Infof("Creating VMI Pod latency watcher for %s", factory.JobConfig.Name)
	p.createPodWatcherIfNotExist()

	log.Infof("Creating VMI latency watcher for %s", factory.JobConfig.Name)
	p.createVMIWatcherIfNotExist()

	log.Infof("Creating VM latency watcher for %s", factory.JobConfig.Name)
	p.createVMWatcherIfNotExist()
}

func (p *vmLatency) createPodWatcherIfNotExist() {
	if _, exist := p.watchers[types.PodWatcher]; !exist {
		w := watcher.NewPodWatcher(
			factory.ClientSet.CoreV1().RESTClient().(*rest.RESTClient),
			v1.NamespaceAll,
			types.VMLatencyMeasurement,
			factory.UUID,
			factory.JobConfig.Name,
		)
		p.RegisterWatcher(types.PodWatcher, w)
	}
}

func (p *vmLatency) createVMWatcherIfNotExist() {
	if _, exist := p.watchers[types.VMWatcher]; !exist {
		restClient := newRESTClientWithRegisteredKubevirtResource(factory.RestConfig)
		w := watcher.NewVMWatcher(
			restClient,
			v1.NamespaceAll,
			types.VMLatencyMeasurement,
			factory.UUID,
			factory.JobConfig.Name,
		)
		p.RegisterWatcher(types.VMWatcher, w)
	}
}

func (p *vmLatency) createVMIWatcherIfNotExist() {
	if _, exist := p.watchers[types.VMIWatcher]; !exist {
		restClient := newRESTClientWithRegisteredKubevirtResource(factory.RestConfig)
		w := watcher.NewVMIWatcher(
			restClient,
			v1.NamespaceAll,
			types.VMLatencyMeasurement,
			factory.UUID,
			factory.JobConfig.Name,
		)
		p.RegisterWatcher(types.VMIWatcher, w)
	}
}

// newRESTClientWithRegisteredKubevirtResource returns a rest client that had KubeVirt CRDs schema registered
func newRESTClientWithRegisteredKubevirtResource(restConf *rest.Config) *rest.RESTClient {
	shallowCopy := setRESTClientConfigDefaults(*restConf)
	restClient, err := rest.RESTClientFor(shallowCopy)
	if err != nil {
		log.Errorf("Error creating custom rest client: %s", err)
		panic(err)
	}
	return restClient
}

func setRESTClientConfigDefaults(config rest.Config) *rest.Config {
	gv := kvv1.SchemeGroupVersion
	shallowCopy := rest.CopyConfig(&config)
	shallowCopy.GroupVersion = &gv
	shallowCopy.APIPath = "/apis"
	scheme := runtime.NewScheme()
	SchemeBuilder := runtime.NewSchemeBuilder(kvv1.AddKnownTypesGenerator(kvv1.GroupVersions))
	AddToScheme := SchemeBuilder.AddToScheme
	codecs := serializer.NewCodecFactory(scheme)
	AddToScheme(scheme)
	shallowCopy.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	return shallowCopy
}

func (p *vmLatency) RegisterWatcher(name string, w *watcher.Watcher) {
	p.watchers[name] = w
}

func (p *vmLatency) GetWatcher(name string) (*watcher.Watcher, bool) {
	w, exists := p.watchers[name]
	return w, exists
}

// Stop stops vmLatency measurement
func (p *vmLatency) Stop() (int, error) {
	p.watchers[types.PodWatcher].StopWatcher()
	p.watchers[types.VMIWatcher].StopWatcher()
	p.watchers[types.VMWatcher].StopWatcher()
	p.normalizeMetrics()
	p.calcQuantiles()
	rc := metrics.CheckThreshold(p.config.LatencyThresholds, p.latencyQuantiles)
	if kubeburnerCfg.WriteToFile {
		if err := metrics.WriteToFile(p.normLatencies, p.latencyQuantiles, "vmLatency", factory.JobConfig.Name, kubeburnerCfg.MetricsDirectory); err != nil {
			log.Errorf("Error writing measurement vmLatency: %s", err)
		}
	}
	if kubeburnerCfg.IndexerConfig.Enabled {
		p.index()
	}
	p.watchers[types.PodWatcher].CleanMetrics()
	p.watchers[types.VMIWatcher].CleanMetrics()
	p.watchers[types.VMWatcher].CleanMetrics()
	return rc, nil
}

// index sends metrics to the configured indexer
func (p *vmLatency) index() {
	(*factory.Indexer).Index(p.config.ESIndex, p.normLatencies)
	(*factory.Indexer).Index(p.config.ESIndex, p.latencyQuantiles)
}

// addAllFieldsFromStructureIntoMap is used to merge the content form different metrics structures
func addAllFieldsFromStructureIntoMap(metric *map[string]interface{}, m interface{}) {
	if metric == nil {
		log.Fatal("Metric map is nill")
	}
	var inInterface map[string]interface{}
	inrec, _ := json.Marshal(m)
	json.Unmarshal(inrec, &inInterface)
	for field, val := range inInterface {
		if val != nil {
			switch val := val.(type) {
			case string:
				(*metric)[field] = val
				continue
			case int:
				(*metric)[field] = val
				continue
			case float64:
				(*metric)[field] = int(val)
			}
		}
	}
}

func (p *vmLatency) normalizeMetrics() {
	normalized := false
	for id, m := range p.watchers[types.VMWatcher].GetMetrics() {
		vmM := m.(*metrics.VMMetric)
		if !vmM.Timestamp.IsZero() {
			metric := make(map[string]interface{})
			if m, err := p.normalizeVMMetrics(id, vmM.Timestamp); err == nil {
				addAllFieldsFromStructureIntoMap(&metric, m)
			}
			if m, err := p.normalizeVMIMetrics(id, vmM.Timestamp, true); err == nil {
				addAllFieldsFromStructureIntoMap(&metric, m)
			}
			if m, err := p.normalizePodMetrics(id, vmM.Timestamp); err == nil {
				addAllFieldsFromStructureIntoMap(&metric, m)
			}
			p.normLatencies = append(p.normLatencies, metric)
			normalized = true
		}
	}
	if !normalized {
		for id, m := range p.watchers[types.VMIWatcher].GetMetrics() {
			vmiM := m.(*metrics.VMIMetric)
			if !vmiM.Timestamp.IsZero() {
				metric := make(map[string]interface{})
				if m, err := p.normalizeVMIMetrics(id, vmiM.Timestamp, true); err == nil {
					addAllFieldsFromStructureIntoMap(&metric, m)
				}
				if m, err := p.normalizePodMetrics(id, vmiM.Timestamp); err == nil {
					addAllFieldsFromStructureIntoMap(&metric, m)
				}
				p.normLatencies = append(p.normLatencies, metric)
			} else {
				log.Debugln("No VM or VMI creation timestamp was collected.")
				return
			}
		}
	}
}

func (p *vmLatency) normalizeVMMetrics(id string, baselineTS time.Time) (interface{}, error) {
	if m, exists := p.watchers[types.VMWatcher].GetMetric(id); exists {
		vmM := m.(*metrics.VMMetric)
		// If the pod did not reach the Ready state (this timestamp wasn't set), the latency will be 0
		if vmM.VMReady != nil {
			vmM.VMReadyLatency = int(vmM.VMReady.Sub(baselineTS).Milliseconds())
		}
		return vmM, nil
	}
	return nil, fmt.Errorf("vm metrics does not exist")
}

func (p *vmLatency) normalizeVMIMetrics(id string, baselineTS time.Time, vmExist bool) (interface{}, error) {
	if m, exists := p.watchers[types.VMIWatcher].GetMetric(id); exists {
		vmiM := m.(*metrics.VMIMetric)
		if vmExist {
			vmiM.VMICreatedLatency = int(vmiM.VMICreated.Sub(baselineTS).Milliseconds())
		}
		vmiM.VMIPendingLatency = int(vmiM.VMIPending.Sub(baselineTS).Milliseconds())
		vmiM.VMISchedulingLatency = int(vmiM.VMIScheduling.Sub(baselineTS).Milliseconds())
		vmiM.VMIScheduledLatency = int(vmiM.VMIScheduled.Sub(baselineTS).Milliseconds())
		// the VMIUnset timestamps may not appear on the object.
		if vmiM.VMIUnset != nil {
			vmiM.VMIUnsetLatency = int(vmiM.VMIUnset.Sub(baselineTS).Milliseconds())
		}
		// the VMIProvisioning timestamps may not appear on the object.
		if vmiM.VMIProvisioning != nil {
			vmiM.VMIProvisioningLatency = int(vmiM.VMIProvisioning.Sub(baselineTS).Milliseconds())
		}
		// the VMISynchronized timestamps may not appear on the object.
		if vmiM.VMISynchronized != nil {
			vmiM.VMISynchronizedLatency = int(vmiM.VMISynchronized.Sub(baselineTS).Milliseconds())
		}
		// the VMIAgentConnected timestamps may not appear on the object.
		if vmiM.VMIAgentConnected != nil {
			vmiM.VMIAgentConnectedLatency = int(vmiM.VMIAgentConnected.Sub(baselineTS).Milliseconds())
		}
		// the VMIAccessCredentialsSynchronized timestamps may not appear on the object.
		if vmiM.VMIAccessCredentialsSynchronized != nil {
			vmiM.VMIAccessCredentialsSynchronizedLatency = int(vmiM.VMIAccessCredentialsSynchronized.Sub(baselineTS).Milliseconds())
		}
		// If the pod did not reach the Running state (this timestamp wasn't set), the latency will be 0
		if vmiM.VMIRunning != nil {
			vmiM.VMIRunningLatency = int(vmiM.VMIRunning.Sub(baselineTS).Milliseconds())
		}
		// If the pod did not reach the Ready state (this timestamp wasn't set), the latency will be 0
		if vmiM.VMIReady != nil {
			vmiM.VMIReadyLatency = int(vmiM.VMIReady.Sub(baselineTS).Milliseconds())
		}
		// the VMISucceeded timestamps may not appear on the object.
		if vmiM.VMISucceeded != nil {
			vmiM.VMISucceededLatency = int(vmiM.VMISucceeded.Sub(baselineTS).Milliseconds())
		}
		// the VMIFailed timestamps may not appear on the object.
		if vmiM.VMIFailed != nil {
			vmiM.VMIFailedLatency = int(vmiM.VMIFailed.Sub(baselineTS).Milliseconds())
		}
		// the VMIUnknown timestamps may not appear on the object.
		if vmiM.VMIUnknown != nil {
			vmiM.VMIUnknownLatency = int(vmiM.VMIUnknown.Sub(baselineTS).Milliseconds())
		}
		return vmiM, nil
	}
	return nil, fmt.Errorf("vmi metrics does not exist")
}

func (p *vmLatency) normalizePodMetrics(id string, baselineTS time.Time) (interface{}, error) {
	if m, exists := p.watchers[types.PodWatcher].GetMetric(id); exists {
		podM := m.(*metrics.PodMetric)
		podM.PodCreatedLatency = int(podM.PodCreated.Sub(baselineTS).Milliseconds())
		podM.PodScheduledLatency = int(podM.PodScheduled.Sub(baselineTS).Milliseconds())
		podM.PodInitializedLatency = int(podM.PodInitialized.Sub(baselineTS).Milliseconds())
		podM.PodContainersReadyLatency = int(podM.PodContainersReady.Sub(baselineTS).Milliseconds())
		// If the pod did not reach the Ready state (this timestamp wasn't set), the latency will be 0
		if podM.PodReady != nil {
			podM.PodReadyLatency = int(podM.PodReady.Sub(baselineTS).Milliseconds())
		}
		return podM, nil
	}
	return nil, fmt.Errorf("vmi pod metrics does not exist")
}

func (p *vmLatency) calcQuantiles() {
	quantiles := []float64{0.5, 0.95, 0.99}
	quantileMap := map[string][]int{}
	for _, normLatency := range p.normLatencies {
		for _, idx := range latencyList {
			if val, exists := normLatency.(map[string]interface{})[idx]; exists {
				quantileMap[idx] = append(quantileMap[idx], val.(int))
			}

		}
	}
	for quantileName, v := range quantileMap {
		quantile := metrics.LatencyQuantiles{
			QuantileName: quantileName,
			UUID:         factory.UUID,
			Timestamp:    time.Now().UTC(),
			JobName:      factory.JobConfig.Name,
			MetricName:   types.VMLatencyMeasurement,
		}
		sort.Ints(v)
		length := len(v)
		if length > 1 {
			for _, q := range quantiles {
				qValue := v[int(math.Ceil(float64(length)*q))-1]
				quantile.SetQuantile(q, qValue)
			}
			quantile.Max = v[length-1]
		}
		sum := 0
		for _, n := range v {
			sum += n
		}
		quantile.Avg = int(math.Round(float64(sum) / float64(length)))
		p.latencyQuantiles = append(p.latencyQuantiles, quantile)
	}
}

func (p *vmLatency) validateConfig() error {
	var metricFound bool
	var latencyMetrics []string = []string{"P99", "P95", "P50", "Avg", "Max"}
	for _, th := range p.config.LatencyThresholds {
		if th.ConditionType == string(kvv1.Pending) ||
			th.ConditionType == string(kvv1.Scheduling) ||
			th.ConditionType == string(kvv1.Scheduled) ||
			th.ConditionType == string(kvv1.Running) ||
			th.ConditionType == string(kvv1.VirtualMachineInstanceReady) ||
			th.ConditionType == string(kvv1.Succeeded) {
			for _, lm := range latencyMetrics {
				if th.Metric == lm {
					metricFound = true
					break
				}
			}
			if !metricFound {
				return fmt.Errorf("unsupported metric %s in vmLatency measurement, supported are: %s", th.Metric, strings.Join(latencyMetrics, ", "))
			}
		} else {
			return fmt.Errorf("unsupported vm condition type in vmLatency measurement: %s", th.ConditionType)
		}
	}
	return nil
}
