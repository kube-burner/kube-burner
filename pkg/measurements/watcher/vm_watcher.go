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
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	kvv1 "kubevirt.io/api/core/v1"
)

func (p *Watcher) handleCreateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmID := getVMID(*vm)
	if _, exists := p.GetMetric(vmID); !exists {
		if strings.Contains(vm.Namespace, jobNamespace) {
			vmM := metrics.VMMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  vm.Namespace,
				Name:       vm.Name,
				MetricName: jobMetricName,
				UUID:       jobUUID,
				JobName:    jobName,
			}
			p.AddMetric(vmID, &vmM)
		}
	}
}

func (p *Watcher) handleUpdateVM(obj interface{}) {
	vm := obj.(*kvv1.VirtualMachine)
	vmID := getVMID(*vm)
	if m, exists := p.GetMetric(vmID); exists && m != nil {
		if m.(*metrics.VMMetric).VMReady == nil {
			vmM := m.(*metrics.VMMetric)
			for _, c := range vm.Status.Conditions {
				if c.Status == v1.ConditionTrue {
					if c.Type == kvv1.VirtualMachineReady {
						t := time.Now().UTC()
						vmM.VMReady = &t
						p.AddResourceStatePerNS(btypes.VirtualMachineResource, "Ready", vm.Namespace, 1)
					}
				}
			}
			p.AddMetric(vmID, vmM)
		}
	}
}

func NewVMWatcher(restClient *rest.RESTClient, namespace string, resourceMetricName string, uuid string, name string) *Watcher {
	jobNamespace = namespace
	jobMetricName = resourceMetricName
	jobUUID = uuid
	jobName = name
	w := NewWatcher(
		restClient,
		mtypes.VMWatcher,
		btypes.VirtualMachineResource,
		namespace,
	)
	w.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: w.handleCreateVM,
		UpdateFunc: func(oldObj, newObj interface{}) {
			w.handleUpdateVM(newObj)
		},
	})
	if err := w.StartAndCacheSync(); err != nil {
		log.Errorf("VirtualMachine watcher start and cache sync error: %s", err)
	}
	return w
}
