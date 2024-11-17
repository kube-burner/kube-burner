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
)

type statusPath struct {
	expectedReplicasPath []string
	readyReplicasPath    []string
}

var waitStatusMap map[string]statusPath = map[string]statusPath{
	StatefulSet: {
		expectedReplicasPath: []string{"spec", "replicas"}, readyReplicasPath: []string{"status", "readyReplicas"}},
	Deployment: {
		expectedReplicasPath: []string{"spec", "replicas"}, readyReplicasPath: []string{"status", "readyReplicas"}},
	DaemonSet: {
		expectedReplicasPath: []string{"status", "desiredNumberScheduled"}, readyReplicasPath: []string{"status", "numberReady"}},
	ReplicaSet: {
		expectedReplicasPath: []string{"spec", "replicas"}, readyReplicasPath: []string{"status", "readyReplicas"}},
	ReplicationController: {
		expectedReplicasPath: []string{"spec", "replicas"}, readyReplicasPath: []string{"status", "replicas"}},
	VirtualMachineInstanceReplicaSet: {
		expectedReplicasPath: []string{"spec", "replicas"}, readyReplicasPath: []string{"status", "readyReplicas"},
	},
}
