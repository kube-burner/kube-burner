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

var (
	timeLayout = "2006-01-02T15:04:05.000Z"
)

func (p *Watcher) handleCreateVMI(obj interface{}) {
	vmi := obj.(*kvv1.VirtualMachineInstance)
	vmiID := getVMIID(*vmi)
	if _, exists := p.GetMetric(vmiID); !exists {
		if strings.Contains(vmi.Namespace, jobNamespace) {
			vmiM := metrics.VMIMetric{
				Timestamp:  time.Now().UTC(),
				Namespace:  vmi.Namespace,
				Name:       vmi.Name,
				MetricName: jobMetricName,
				UUID:       jobUUID,
				JobName:    jobName,
			}
			p.AddMetric(vmiID, &vmiM)
		}
	}
	if m, exists := p.GetMetric(vmiID); exists {
		vmiM := m.(*metrics.VMIMetric)
		if vmiM.VMICreated == nil {
			t := time.Now().UTC()
			vmiM.VMICreated = &t
		}
		p.AddMetric(vmiID, vmiM)
	}
}

func (p *Watcher) handleUpdateVMI(obj interface{}) {
	vmi := obj.(*kvv1.VirtualMachineInstance)
	vmiID := getVMIID(*vmi)
	if m, exists := p.GetMetric(vmiID); exists && m != nil {
		if m.(*metrics.VMIMetric).VMIReady == nil {
			vmiM := m.(*metrics.VMIMetric)
			for _, c := range vmi.Status.Conditions {
				if c.Status == v1.ConditionTrue {
					switch c.Type {
					case kvv1.VirtualMachineInstanceProvisioning:
						if vmiM.VMIProvisioning == nil {
							t := time.Now().UTC()
							vmiM.VMIProvisioning = &t
						}
					case kvv1.VirtualMachineInstanceSynchronized:
						if vmiM.VMISynchronized == nil {
							t := time.Now().UTC()
							vmiM.VMISynchronized = &t
						}
					case kvv1.VirtualMachineInstanceAgentConnected:
						if vmiM.VMIAgentConnected == nil {
							t := time.Now().UTC()
							vmiM.VMIAgentConnected = &t
						}
					case kvv1.VirtualMachineInstanceAccessCredentialsSynchronized:
						if vmiM.VMIAccessCredentialsSynchronized == nil {
							t := time.Now().UTC()
							vmiM.VMIAccessCredentialsSynchronized = &t
						}
					case kvv1.VirtualMachineInstanceReady:

						if vmiM.VMIReady == nil {
							t := time.Now().UTC()
							vmiM.VMIReady = &t
							log.Debugf("VMI %s is Ready", vmi.Name)
							p.AddResourceStatePerNS(btypes.VirtualMachineInstanceResource, "Ready", vmi.Namespace, 1)
						}
					}
				}
			}

			// Although the pattern of using phase is deprecated, kubevirt still strongly relies on it.
			switch vmi.Status.Phase {
			case kvv1.VmPhaseUnset:
				if vmiM.VMIUnset == nil {
					t := time.Now().UTC()
					vmiM.VMIUnset = &t
				}
			case kvv1.Pending:
				if vmiM.VMIPending == nil {
					t := time.Now().UTC()
					vmiM.VMIPending = &t
				}
			case kvv1.Scheduling:
				if vmiM.VMIScheduling == nil {
					t := time.Now().UTC()
					vmiM.VMIScheduling = &t
				}
			case kvv1.Scheduled:
				if vmiM.VMIScheduled == nil {
					t := time.Now().UTC()
					vmiM.VMIScheduled = &t
				}
			case kvv1.Running:
				if vmiM.VMIRunning == nil {
					t := time.Now().UTC()
					vmiM.VMIRunning = &t
				}
			case kvv1.Succeeded:
				if vmiM.VMISucceeded == nil {
					t := time.Now().UTC()
					vmiM.VMISucceeded = &t
				}
			case kvv1.Failed:
				if vmiM.VMIFailed == nil {
					t := time.Now().UTC()
					vmiM.VMIFailed = &t
				}
			case kvv1.Unknown:
				if vmiM.VMIUnknown == nil {
					t := time.Now().UTC()
					vmiM.VMIUnknown = &t
				}
			}
		}
	}
}

func NewVMIWatcher(restClient *rest.RESTClient, namespace string, resourceMetricName string, uuid string, name string) *Watcher {
	jobNamespace = namespace
	jobMetricName = resourceMetricName
	jobUUID = uuid
	jobName = name
	w := NewWatcher(
		restClient,
		mtypes.VMIWatcher,
		btypes.VirtualMachineInstanceResource,
		namespace,
	)
	w.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: w.handleCreateVMI,
		UpdateFunc: func(oldObj, newObj interface{}) {
			w.handleUpdateVMI(newObj)
		},
	})
	if err := w.StartAndCacheSync(); err != nil {
		log.Errorf("VirtualMachineInstance watcher start and cache sync error: %s", err)
	}
	return w
}
