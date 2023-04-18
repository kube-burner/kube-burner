# OpenShift Wrapper

The kube-burner binary brings a very opinionated OpenShift wrapper designed to simplify the execution of different workloads in this kubernetes distribution.
This wrapper is hosted under the `kube-burner ocp` subcommand that currently looks like:

```console
$ kube-burner ocp -h
This subcommand is meant to be used against OpenShift clusters and serve as a shortcut to trigger well-known workloads

Usage:
  kube-burner ocp [command]

Available Commands:
  cluster-density    Runs cluster-density workload
  cluster-density-ms Runs cluster-density-ms workload
  cluster-density-v2 Runs cluster-density-v2 workload
  node-density       Runs node-density workload
  node-density-cni   Runs node-density-cni workload
  node-density-heavy Runs node-density-heavy workload

Flags:
      --alerting                  Enable alerting (default true)
      --burst int                 Burst (default 20)
      --es-index string           Elastic Search index
      --es-server string          Elastic Search endpoint
      --extract                   Extract workload in the current directory
      --gc                        Garbage collect created namespaces (default true)
  -h, --help                      help for ocp
      --local-indexing            Enable local indexing
      --metrics-endpoint string   YAML file with a list of metric endpoints
      --qps int                   QPS (default 20)
      --timeout duration          Benchmark timeout (default 3h0m0s)
      --user-metadata string      User provided metadata file, in YAML format
      --uuid string               Benchmark UUID (default "d18989c4-4f8a-4a14-b711-9afae69a9140")

Global Flags:
      --log-level string   Allowed values: debug, info, warn, error, fatal (default "info")

Use "kube-burner ocp [command] --help" for more information about a command.
```

## Usage

In order to trigger one of the supported workloads using this subcommand you have to run kube-burner using the subcommand ocp. The workloads are embed in the kube-burner binary:

Running node-density with 100 pods per node

```console
kube-burner ocp node-density --pods-per-node=100
```

Running cluster-density with multiple endpoints support

```console
kube-burner ocp cluster-density --iterations=1 --churn-duration=2m0s --es-index kube-burner --es-server https://www.esurl.com:443 --metrics-endpoint metrics-endpoints.yaml
```

With the command above, the wrapper will calculate the required number of pods to deploy across all worker nodes of the cluster.

This wrapper provides the following benefits among others:

- Provides a simplified execution of the supported workloads
- Indexes OpenShift metadata along with the Benchmark result, this document can be found with the following query: `uuid: <benchmkark-uuid> AND metricName.keyword: "clusterMetadata"`
- Prevents modifying configuration files to tweak some of the parameters of the workloads
- Discovers the Prometheus URL and authentication token, so the user does not have to perform those operations before using them.

## Available workloads

### cluster-density, cluster-density-v2 and cluster-density-ms

Control-plane density focused test that creates deployments, builds, secrets, services and more across in the cluster. Each iteration of these workloads create a new namespace, they support the following flags.

```shell
$ kube-burner ocp cluster-density -h
Runs cluster-density workload

Usage:
  kube-burner ocp cluster-density [flags]

Flags:
      --churn                     Enable churning (default true)
      --churn-delay duration      Time to wait between each churn (default 2m0s)
      --churn-duration duration   Churn duration (default 1h0m0s)
      --churn-percent int         Percentage of job iterations that kube-burner will churn each round (default 10)
      --iterations int            cluster-density iterations
```

---

Each iteration of **cluster-density** creates the following objects in each of the namespaces/iterations:

- 1 imagestream
- 1 build. The OCP internal container registry must be set-up previously since the resulting container image will be pushed there.
- 5 deployments with two pod replicas (pause) mounting 4 secrets, 4 configmaps and 1 downwardAPI volume each
- 5 services, each one pointing to the TCP/8080 and TCP/8443 ports of one of the previous deployments
- 1 edge route pointing to the to first service
- 10 secrets containing 2048 character random string
- 10 configMaps containing a 2048 character random string

---

Each iteration of **cluster-density-v2** creates the following objects in each of the namespaces/iterations:

- 1 imagestream
- 1 build. The OCP internal container registry must be set-up previously since the resulting container image will be pushed there.
- 3 deployments with two pod 2 replicas (nginx) mounting 4 secrets, 4 configmaps and 1 downwardAPI volume each
- 2 deployments with two pod 2 replicas (curl) mounting 4 secrets, 4 configmaps and 1 downwardAPI volume each. These pods have configured a readinessProbe that makes a request to one of the services and one of the routes created by this workload every 10 seconds.
- 5 services, each one pointing to the TCP/8080 port of one of the nginx deployments.
- 2 edge route pointing to the to first and second service respectively
- 10 secrets containing 2048 character random string
- 10 configMaps containing a 2048 character random string
- 3 network policies
  - 1 deny-all traffic
  - 1 allow traffic from client/nginx pods to server/nginx pods
  - 1 allow traffic from openshift-ingress namespace (where routers are deployed by default) to the namespace

---

Each iteration of **cluster-density-ms** creates the following objects in each of the namespaces/iterations:

- 1 imagestream
- 4 deployments with two pod replicas (pause) mounting 4 secrets, 4 configmaps and 1 downwardAPI volume each
- 2 services, each one pointing to the TCP/8080 and TCP/8443 ports of the first and second deployment respectively.
- 1 edge route pointing to the to first service
- 20 secrets containing 2048 character random string
- 10 configMaps containing a 2048 character random string

## node-density, node-density-cni & node-density-heavy

The **node-density** workload is meant to fill with pause pods all the worker nodes from the cluster. It can be customized with the following flags.

```shell
$ kube-burner ocp node-density -h
Runs node-density workload

Usage:
  kube-burner ocp node-density [flags]

Flags:
      --container-image string         Container image (default "gcr.io/google_containers/pause:3.1")
  -h, --help                           help for node-density
      --pod-ready-threshold duration   Pod ready timeout threshold (default 5s)
      --pods-per-node int              Pods per node (default 245)
```

---

The **node-density-cni** workload does something similar with the difference that it creates two deployments client/curl and server/nxing, and 1 service backed by the previous server deployment. The client application has configured an startupProbe that makes requests to the service every second with a timeout of 600s.

$ kube-burner ocp node-density-cni -h
Runs node-density-cni workload

```shell
Usage:
  kube-burner ocp node-density-cni [flags]

Flags:
  -h, --help                help for node-density-cni
      --pods-per-node int   Pods per node (default 245)
```

---

The **node-density-heavy** workload creates a single namespace with two deployments, a postgresql database and a simple client that performs periodic queries on the previous database and a service that is used by the client to reach the database.

```shell
$ kube-burner ocp node-density-heavy -h
Runs node-density-heavy workload

Usage:
  kube-burner ocp node-density-heavy [flags]

Flags:
  -h, --help                           help for node-density-heavy
      --pod-ready-threshold duration   Pod ready timeout threshold (default 1h0m0s)
      --pods-per-node int              Pods per node (default 245)
      --probes-period int              Perf app readiness/livenes probes period in seconds (default 10)
```

## Customizing workloads

It's possible to customize the workload configuration before running the workload by extracting, updating and finally running it:

```console
$ kube-burner ocp node-density --extract
$ ls
alerts.yml  metrics.yml  node-density.yml  pod.yml
$ vi node-density.yml                               # Perform modifications accordingly
$ kube-burner ocp node-density --pods-per-node=100  # Run workload
```

### Cluster metadata

As soon as a benchmark finishes, kube-burner will index the cluster metadata in the configured indexer. At the time of writing this document is based on the following golang struct:

```golang
type clusterMetadata struct {
 MetricName       string                 `json:"metricName,omitempty"`
 UUID             string                 `json:"uuid"`
 Platform         string                 `json:"platform"`
 OCPVersion       string                 `json:"ocpVersion"`
 K8SVersion       string                 `json:"k8sVersion"`
 MasterNodesType  string                 `json:"masterNodesType"`
 WorkerNodesType  string                 `json:"workerNodesType"`
 InfraNodesType   string                 `json:"infraNodesType"`
 WorkerNodesCount int                    `json:"workerNodesCount"`
 InfraNodesCount  int                    `json:"infraNodesCount"`
 TotalNodes       int                    `json:"totalNodes"`
 SDNType          string                 `json:"sdnType"`
 Benchmark        string                 `json:"benchmark"`
 Timestamp        time.Time              `json:"timestamp"`
 EndDate          time.Time              `json:"endDate"`
 ClusterName      string                 `json:"clusterName"`
 Passed           bool                   `json:"passed"`
 Metadata         map[string]interface{} `json:"metadata,omitempty"`
}
```

Where metricName is hardcoded to `clusterMetadata`
