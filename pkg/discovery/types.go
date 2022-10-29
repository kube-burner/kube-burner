package discovery

type infraObj struct {
	Status struct {
		InfrastructureName string `json:"infrastructureName"`
		Platform           string `json:"platform"`
	} `json:"status"`
}

type versionObj struct {
	OcpVersion string
	K8sVersion string
}

type clusterVersion struct {
	Status struct {
		History []struct {
			State   string `json:"state"`
			Version string `json:"version"`
		} `json:"history"`
	} `json:"status"`
}

type nodeInfo struct {
	WorkerCount int
	InfraCount  int
	TotalNodes  int
	MasterType  string
	WorkerType  string
	InfraType   string
}

const (
	routeGroup      = "route.openshift.io"
	routeVersion    = "v1"
	routeResource   = "routes"
	completedUpdate = "Completed"
)
