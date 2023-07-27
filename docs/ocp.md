# OpenShift Wrapper

The kube-burner binary brings a very opinionated OpenShift wrapper designed to simplify the execution of different workloads in this kubernetes distribution.
This wrapper is hosted under the `kube-burner ocp` subcommand that currently looks like:

```console
$ kube-burner ocp help
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
      --reporting                 Enable benchmark report indexing
      --timeout duration          Benchmark timeout (default 4h0m0s)
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

## Cluster density workloads

This workload family is a control-plane density focused workload that that creates different objects across the cluster. There're 3 different variants [cluster-density](#cluster-density), [cluster-density-v2](#cluster-density-v2) and [cluster-density-ms](#cluster-density-ms).

Each iteration of these create a new namespace, the three support similar configuration flags. Check them out from the subcommand help

!!! Info
    Workload churning of 1h is enabled by default in the `cluster-density` workloads, you can disable it by passing `--churn=false` to the workload subcommand

### cluster-density

Each iteration of **cluster-density** creates the following objects in each of the dreated namespaces:

- 1 Imagestream
- 1 Build. The OCP internal container registry must be set-up previously since the resulting container image will be pushed there.
- 5 Deployments with two pod replicas (pause) mounting 4 secrets, 4 configmaps and 1 downwardAPI volume each
- 5 Services, each one pointing to the TCP/8080 and TCP/8443 ports of one of the previous deployments
- 1 edge Route pointing to the to first service
- 10 Secrets containing 2048 character random string
- 10 ConfigMaps containing a 2048 character random string

### cluster-density-v2

Very similar to [cluster-density](#cluster-density), but with some key differences provided by NetworkPolicies and improved readinessProbes, that leads to a heavier load in the cluster's CNI plugin. Each iteration creates the following objects in each of the created namespaces:

- 1 ImagesStream
- 1 Build. The OCP internal container registry must be set-up previously since the resulting container image will be pushed there.
- 3 Deployments with two pod 2 replicas (nginx) mounting 4 Secrets, 4 configmaps and 1 downwardAPI volume each
- 2 Deployments with two pod 2 replicas (curl) mounting 4 Secrets, 4 configmaps and 1 downwardAPI volume each. These pods have configured a readinessProbe that makes a request to one of the Services and one of the routes created by this workload every 10 seconds.
- 5 Services, each one pointing to the TCP/8080 port of one of the nginx Deployments.
- 2 edge Routes pointing to the to first and second Services respectively
- 10 Secrets containing 2048 character random string
- 10 ConfigMaps containing a 2048 character random string
- 3 NetworkPolicies:
    - deny-all traffic
    - allow traffic from client/nginx pods to server/nginx pods
    - allow traffic from openshift-ingress namespace (where routers are deployed by default) to the namespace

### cluster-density-ms

Lightest version of this workload family, each iteration the following objects in each of the created namespaces:

- 1 ImageStream
- 4 Deployments with two pod replicas (pause) mounting 4 secrets, 4 configmaps and 1 downwardAPI volume each
- 2 Services, each one pointing to the TCP/8080 and TCP/8443 ports of the first and second deployment respectively.
- 1 edge Route pointing to the to first service
- 20 Secrets containing 2048 character random string
- 10 ConfigMaps containing a 2048 character random string

## Node density workloads

The workloads of this family create a single namespace with a set of Pods, Deployments Services, depending on the workload.

### node-density

This workload is meant to fill with pause pods all the worker nodes from the cluster. It can be customized with the following flags. This workload is usually used to measure the Pod's ready latency KPI.

### node-density-cni

It creates two Deployments, a client/curl and a server/nxing, and 1 Service backed by the previous server Pods. The client application has configured an startupProbe that makes requests to the previous Service every second with a timeout of 600s.

Note: this workload calculates the number of iterations to create from the number of nodes and desired pods per node.  In order to keep the test scalable and performant, chunks of 1000 iterations will by broken into separate namespaces, using the config variable `iterationsPerNamespace`.

### node-density-heavy

Creates two Deployments, a postgresql database and a simple client that performs periodic insert queries (configured through liveness and readiness probes) on the previous database and a Service that is used by the client to reach the database.

Note: this workload calculates the number of iterations to create from the number of nodes and desired pods per node.  In order to keep the test scalable and performant, chunks of 1000 iterations will by broken into separate namespaces, using the config variable `iterationsPerNamespace`.

## Reporting mode

This mode can be enabled with the flag `--reporting`. By enabling this mode kube-burner will a metrics-profile and will index the [aggregated values of the defined timeseries](/kube-burner/metrics/observability/metrics/#aggregating-timeseries-into-a-single-document), and will index only the pod latency quantiles documents (`podLatencyQuantilesMeasurement`) rather than the full pod timeseries.

This feature is very useful to avoid sending thousands of documents to the configured indexer, as only a few documents will be indexed per benchmark. The metrics profile used by this feature is defined in [metrics-report.yml](https://github.com/cloud-bulldozer/kube-burner/cmd/ocp-config/metrics-report.yml))

## Customizing workloads

It's possible to customize any of the above workload configurations by extracting, updating and finally running it:

```console
$ kube-burner ocp node-density --extract
$ ls
alerts.yml  metrics.yml  node-density.yml  pod.yml  metrics-report.yml
$ vi node-density.yml                               # Perform modifications accordingly
$ kube-burner ocp node-density --pods-per-node=100  # Run workload
```

## Cluster metadata

When the benchmark finishes, kube-burner will index the cluster metadata in the configured indexer. At the time of writing this document is based on the following golang struct:

```golang
type BenchmarkMetadata struct {
  ocpmetadata.ClusterMetadata
  UUID         string                 `json:"uuid"`
  Benchmark    string                 `json:"benchmark"`
  Timestamp    time.Time              `json:"timestamp"`
  EndDate      time.Time              `json:"endDate"`
  Passed       bool                   `json:"passed"`
  UserMetadata map[string]interface{} `json:"metadata,omitempty"`
}
```

Where `ocpmetadata.ClusterMetadata` is an embed struct inherited from the [go-commons library](https://github.com/cloud-bulldozer/go-commons/blob/main/ocp-metadata/types.go) which has the following fields:

```golang
// Type to store cluster metadata
type ClusterMetadata struct {
  MetricName       string `json:"metricName,omitempty"`
  Platform         string `json:"platform"`
  OCPVersion       string `json:"ocpVersion"`
  OCPMajorVersion  string `json:"ocpMajorVersion"`
  K8SVersion       string `json:"k8sVersion"`
  MasterNodesType  string `json:"masterNodesType"`
  WorkerNodesType  string `json:"workerNodesType"`
  MasterNodesCount int    `json:"masterNodesCount"`
  InfraNodesType   string `json:"infraNodesType"`
  WorkerNodesCount int    `json:"workerNodesCount"`
  InfraNodesCount  int    `json:"infraNodesCount"`
  TotalNodes       int    `json:"totalNodes"`
  SDNType          string `json:"sdnType"`
  ClusterName      string `json:"clusterName"`
  Region           string `json:"region"`
}
```

MetricName is hardcoded to `clusterMetadata`

<!-- markdownlint-disable -->
!!! Info
    It's important to note that every document indexed when using an OCP wrapper workload will include an small subset of the previous fields:
    ```yaml
    platform
    ocpVersion
    ocpMajorVersion
    k8sVersion
    totalNodes
    sdnType
    ```
<!-- markdownlint-restore -->