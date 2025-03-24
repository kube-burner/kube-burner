package burner

const (
	StatefulSet                      = "StatefulSet"
	Deployment                       = "Deployment"
	DaemonSet                        = "DaemonSet"
	ReplicaSet                       = "ReplicaSet"
	Job                              = "Job"
	Pod                              = "Pod"
	ReplicationController            = "ReplicationController"
	Build                            = "Build"
	BuildConfig                      = "BuildConfig"
	VirtualMachine                   = "VirtualMachine"
	VirtualMachineInstance           = "VirtualMachineInstance"
	VirtualMachineInstanceReplicaSet = "VirtualMachineInstanceReplicaSet"
	PersistentVolumeClaim            = "PersistentVolumeClaim"
	VolumeSnapshot                   = "VolumeSnapshot"
	DataVolume                       = "DataVolume"
)

type statusPath struct {
	expectedReplicasPath []string
	readyReplicasPath    []string
}

var pathSpecReplicas = []string{"spec", "replicas"}
var statusReplicas = []string{"status", "readyReplicas"}

var waitStatusMap map[string]statusPath = map[string]statusPath{
	StatefulSet: {
		expectedReplicasPath: pathSpecReplicas, readyReplicasPath: statusReplicas},
	Deployment: {
		expectedReplicasPath: pathSpecReplicas, readyReplicasPath: statusReplicas},
	DaemonSet: {
		expectedReplicasPath: []string{"status", "desiredNumberScheduled"}, readyReplicasPath: []string{"status", "numberReady"}},
	ReplicaSet: {
		expectedReplicasPath: pathSpecReplicas, readyReplicasPath: statusReplicas},
	ReplicationController: {
		expectedReplicasPath: pathSpecReplicas, readyReplicasPath: statusReplicas},
	VirtualMachineInstanceReplicaSet: {
		expectedReplicasPath: pathSpecReplicas, readyReplicasPath: statusReplicas,
	},
}
