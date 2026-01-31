package measurements

import (
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestHandleUpdatePVC(t *testing.T) {
	p := &pvcLatency{
		BaseMeasurement: BaseMeasurement{
			Metrics: sync.Map{},
		},
	}

	pvcUID := types.UID("test-pvc-uid")
	pvcName := "test-pvc"

	// Initial creation
	pm := pvcMetric{
		Name:      pvcName,
		Namespace: "default",
		Size:      "1Gi",
	}
	p.Metrics.Store(string(pvcUID), pm)

	// Helper to convert PVC to unstructured
	toUnstructured := func(pvc *corev1.PersistentVolumeClaim) *unstructured.Unstructured {
		unstr, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(pvc)
		return &unstructured.Unstructured{Object: unstr}
	}

	// 1. Move to Pending
	pvcPending := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			UID:  pvcUID,
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}
	p.handleUpdatePVC(toUnstructured(pvcPending))

	val, _ := p.Metrics.Load(string(pvcUID))
	pm = val.(pvcMetric)
	if pm.pending == 0 {
		t.Fatal("Expected pending timestamp to be set")
	}
	pendingTime := pm.pending

	// 2. Move to Bound
	pvcBound := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			UID:  pvcUID,
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
	p.handleUpdatePVC(toUnstructured(pvcBound))

	val, _ = p.Metrics.Load(string(pvcUID))
	pm = val.(pvcMetric)
	if pm.bound == 0 {
		t.Fatal("Expected bound timestamp to be set")
	}
	boundTime := pm.bound

	// 3. Try another phase update (it should be skipped)
	time.Sleep(2 * time.Millisecond)
	p.handleUpdatePVC(toUnstructured(pvcPending))

	val, _ = p.Metrics.Load(string(pvcUID))
	pm = val.(pvcMetric)
	if pm.pending != pendingTime {
		t.Errorf("Pending time changed from %v to %v after PVC was already bound", pendingTime, pm.pending)
	}
	if pm.bound != boundTime {
		t.Errorf("Bound time changed")
	}

	// 4. Test Resize started by Spec change
	pvcResize := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			UID:  pvcUID,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
	p.handleUpdatePVC(toUnstructured(pvcResize))
	val, _ = p.Metrics.Load(string(pvcUID))
	pm = val.(pvcMetric)
	if pm.resizeStarted == 0 {
		t.Fatal("Expected resizeStarted timestamp to be set on spec change")
	}

	// 5. Complete Resize
	pvcResized := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName,
			UID:  pvcUID,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("2Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("2Gi"),
			},
		},
	}
	p.handleUpdatePVC(toUnstructured(pvcResized))
	val, _ = p.Metrics.Load(string(pvcUID))
	pm = val.(pvcMetric)
	if pm.ResizeLatency == 0 {
		t.Fatal("Expected ResizeLatency to be set")
	}
	if pm.ResizedCapacity != "2Gi" {
		t.Errorf("Expected ResizedCapacity to be 2Gi, got %v", pm.ResizedCapacity)
	}

	// 6. Verification with Lost state
	pvcUID2 := types.UID("test-pvc-uid-2")
	p.Metrics.Store(string(pvcUID2), pvcMetric{Name: "lost-pvc", Size: "1Gi"})

	pvcLost := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "lost-pvc",
			UID:  pvcUID2,
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimLost,
		},
	}
	p.handleUpdatePVC(toUnstructured(pvcLost))
	val, _ = p.Metrics.Load(string(pvcUID2))
	pm = val.(pvcMetric)
	if pm.lost == 0 {
		t.Fatal("Expected lost timestamp to be set")
	}
	lostTime := pm.lost

	p.handleUpdatePVC(toUnstructured(pvcBound))
	val, _ = p.Metrics.Load(string(pvcUID2))
	pm = val.(pvcMetric)
	if pm.bound != 0 {
		t.Error("Expected bound timestamp to NOT be set after PVC was already lost")
	}
	if pm.lost != lostTime {
		t.Error("Lost timestamp changed")
	}
}
