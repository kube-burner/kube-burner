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

package burner

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubevirtV1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/util"
)

const (
	kubeVirtAPIVersionV1 = "kubevirt.io/v1"
	kubeVirtDefaultKind  = "VirtualMachine"
	maxRetries           = 15
	concurrentError      = "the server rejected our request due to an error in our request"
)

var supportedOps = map[config.KubeVirtOpType]struct{}{
	config.KubeVirtOpStart:        {},
	config.KubeVirtOpStop:         {},
	config.KubeVirtOpRestart:      {},
	config.KubeVirtOpPause:        {},
	config.KubeVirtOpUnpause:      {},
	config.KubeVirtOpMigrate:      {},
	config.KubeVirtOpAddVolume:    {},
	config.KubeVirtOpRemoveVolume: {},
}

func (ex *Executor) setupKubeVirtJob(configSpec config.Spec, mapper meta.RESTMapper) {
	var err error

	if len(ex.ExecutionMode) == 0 {
		ex.ExecutionMode = config.ExecutionModeSequential
	}
	ex.itemHandler = kubeOpHandler

	ex.kubeVirtClient, err = kubecli.GetKubevirtClientFromRESTConfig(ex.restConfig)
	if err != nil {
		log.Fatalf("Failed to get kubevirt client - %v", err)
	}

	for _, o := range ex.Objects {
		if len(o.KubeVirtOp) == 0 {
			log.Fatalln("Empty kubeVirtOp not allowed")
		}
		if _, ok := supportedOps[o.KubeVirtOp]; !ok {
			log.Fatalf("Unsupported KubeVirtOp: %s", o.KubeVirtOp)
		}

		if len(o.Kind) == 0 {
			o.Kind = kubeVirtDefaultKind
		}

		ex.objects = append(ex.objects, newObject(o, configSpec, mapper, kubeVirtAPIVersionV1))
	}
}

func kubeOpHandler(ex *Executor, obj object, item unstructured.Unstructured, iteration int, wg *sync.WaitGroup) {
	defer wg.Done()

	var err error
	switch obj.KubeVirtOp {
	case config.KubeVirtOpStart:
		options := kubevirtV1.StartOptions{}
		startPaused := util.GetBoolValue(obj.InputVars, "startPaused")
		if startPaused != nil {
			options.Paused = *startPaused
		}
		err = ex.kubeVirtClient.VirtualMachine(item.GetNamespace()).Start(context.Background(), item.GetName(), &options)
	case config.KubeVirtOpStop:
		stopOpts := &kubevirtV1.StopOptions{}
		force := util.GetBoolValue(obj.InputVars, "force")
		if force != nil && *force {
			gracePeriod := int64(0)
			stopOpts.GracePeriod = &gracePeriod
		}
		err = ex.kubeVirtClient.VirtualMachine(item.GetNamespace()).Stop(context.Background(), item.GetName(), stopOpts)
	case config.KubeVirtOpRestart:
		restartOpts := &kubevirtV1.RestartOptions{}
		force := util.GetBoolValue(obj.InputVars, "force")
		if force != nil && *force {
			gracePeriod := int64(0)
			restartOpts.GracePeriodSeconds = &gracePeriod
		}
		err = ex.kubeVirtClient.VirtualMachine(item.GetNamespace()).Restart(context.Background(), item.GetName(), restartOpts)
	case config.KubeVirtOpPause:
		err = ex.kubeVirtClient.VirtualMachineInstance(item.GetNamespace()).Pause(context.Background(), item.GetName(), &kubevirtV1.PauseOptions{})
	case config.KubeVirtOpUnpause:
		err = ex.kubeVirtClient.VirtualMachineInstance(item.GetNamespace()).Unpause(context.Background(), item.GetName(), &kubevirtV1.UnpauseOptions{})
	case config.KubeVirtOpMigrate:
		err = ex.kubeVirtClient.VirtualMachine(item.GetNamespace()).Migrate(context.Background(), item.GetName(), &kubevirtV1.MigrateOptions{})
	case config.KubeVirtOpAddVolume:
		err = addVolume(ex, item.GetName(), item.GetNamespace(), obj.InputVars)
	case config.KubeVirtOpRemoveVolume:
		err = removeVolume(ex, item.GetName(), item.GetNamespace(), obj.InputVars)
	}

	if err != nil {
		log.Errorf("Failed to execute op [%s] on the VM [%s]: %v", obj.KubeVirtOp, item.GetName(), err)
	} else {
		log.Debugf("Successfully executed op [%s] on the VM [%s]", obj.KubeVirtOp, item.GetName())
	}
}

func getVolumeSourceFromVolume(ex *Executor, volumeName, namespace string) (*kubevirtV1.HotplugVolumeSource, error) {
	//Check if data volume exists.
	_, err := ex.kubeVirtClient.CdiClient().CdiV1beta1().DataVolumes(namespace).Get(context.TODO(), volumeName, metav1.GetOptions{})
	if err == nil {
		return &kubevirtV1.HotplugVolumeSource{
			DataVolume: &kubevirtV1.DataVolumeSource{
				Name:         volumeName,
				Hotpluggable: true,
			},
		}, nil
	}
	// DataVolume not found, try PVC
	_, err = ex.kubeVirtClient.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), volumeName, metav1.GetOptions{})
	if err == nil {
		return &kubevirtV1.HotplugVolumeSource{
			PersistentVolumeClaim: &kubevirtV1.PersistentVolumeClaimVolumeSource{
				PersistentVolumeClaimVolumeSource: v1.PersistentVolumeClaimVolumeSource{
					ClaimName: volumeName,
				},
				Hotpluggable: true,
			},
		}, nil
	}
	// Neither return error
	return nil, fmt.Errorf("volume %s is not a DataVolume or PersistentVolumeClaim", volumeName)
}

func addVolume(ex *Executor, vmiName, namespace string, extraArgs map[string]interface{}) error {
	volumeName := util.GetStringValue(extraArgs, "volumeName")
	if volumeName == nil {
		return fmt.Errorf("'volumeName' is mandatory")
	}

	diskTypePtr := util.GetStringValue(extraArgs, "diskType")
	diskType := "disk"
	if diskTypePtr != nil {
		diskType = *diskTypePtr
	}

	serial := util.GetStringValue(extraArgs, "serial")
	cache := util.GetStringValue(extraArgs, "cache")

	persistPtr := util.GetBoolValue(extraArgs, "persist")
	persist := false
	if persistPtr != nil {
		persist = *persistPtr
	}

	volumeSource, err := getVolumeSourceFromVolume(ex, *volumeName, namespace)
	if err != nil {
		return err
	}

	hotplugRequest := &kubevirtV1.AddVolumeOptions{
		Name: *volumeName,
		Disk: &kubevirtV1.Disk{
			DiskDevice: kubevirtV1.DiskDevice{},
		},
		VolumeSource: volumeSource,
	}

	switch diskType {
	case "disk":
		hotplugRequest.Disk.DiskDevice.Disk = &kubevirtV1.DiskTarget{
			Bus: "scsi",
		}
	case "lun":
		hotplugRequest.Disk.DiskDevice.LUN = &kubevirtV1.LunTarget{
			Bus: "scsi",
		}
	default:
		return fmt.Errorf("invalid disk type '%s'. Only LUN and Disk are supported", diskType)
	}

	if serial != nil {
		hotplugRequest.Disk.Serial = *serial
	} else {
		hotplugRequest.Disk.Serial = *volumeName
	}
	if cache != nil {
		hotplugRequest.Disk.Cache = kubevirtV1.DriverCache(*cache)
		// Verify if cache mode is valid
		if hotplugRequest.Disk.Cache != kubevirtV1.CacheNone &&
			hotplugRequest.Disk.Cache != kubevirtV1.CacheWriteThrough &&
			hotplugRequest.Disk.Cache != kubevirtV1.CacheWriteBack {
			return fmt.Errorf("error adding volume, invalid cache value %s", *cache)
		}
	}
	retry := 0
	for retry < maxRetries {
		if !persist {
			err = ex.kubeVirtClient.VirtualMachineInstance(namespace).AddVolume(context.Background(), vmiName, hotplugRequest)
		} else {
			err = ex.kubeVirtClient.VirtualMachine(namespace).AddVolume(context.Background(), vmiName, hotplugRequest)
		}
		if err != nil && err.Error() != concurrentError {
			return fmt.Errorf("error adding volume, %v", err)
		}
		if err == nil {
			break
		}
		retry++
		if retry < maxRetries {
			time.Sleep(time.Duration(retry*(rand.IntN(5))) * time.Millisecond)
		}
	}
	if err != nil && retry == maxRetries {
		return fmt.Errorf("error adding volume after %d retries", maxRetries)
	}
	return nil
}

func removeVolume(ex *Executor, vmiName, namespace string, extraArgs map[string]interface{}) error {
	volumeName := util.GetStringValue(extraArgs, "volumeName")
	if volumeName == nil {
		return fmt.Errorf("'volumeName' is mandatory")
	}

	persistPtr := util.GetBoolValue(extraArgs, "persist")
	persist := false
	if persistPtr != nil {
		persist = *persistPtr
	}

	var err error
	retry := 0
	for retry < maxRetries {
		if !persist {
			err = ex.kubeVirtClient.VirtualMachineInstance(namespace).RemoveVolume(context.Background(), vmiName, &kubevirtV1.RemoveVolumeOptions{
				Name: *volumeName,
			})
		} else {
			err = ex.kubeVirtClient.VirtualMachine(namespace).RemoveVolume(context.Background(), vmiName, &kubevirtV1.RemoveVolumeOptions{
				Name: *volumeName,
			})
		}

		if err != nil && err.Error() != concurrentError {
			return fmt.Errorf("error removing volume, %v", err)
		}
		if err == nil {
			break
		}
		retry++
		if retry < maxRetries {
			time.Sleep(time.Duration(retry*(rand.IntN(5))) * time.Millisecond)
		}
	}

	if err != nil && retry == maxRetries {
		return fmt.Errorf("error removing volume after %d retries", maxRetries)
	}

	log.Debugf("Successfully submitted remove volume request to VM %s for volume %s", vmiName, *volumeName)

	return nil
}
