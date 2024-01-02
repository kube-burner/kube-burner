# OpenShift Wrapper

The kube-burner binary brings a very opinionated OpenShift wrapper designed to simplify the execution of different workloads in this Kubernetes distribution.
This wrapper is hosted under the `kube-burner ocp` subcommand that currently looks like:

```console
$ kube-burner ocp help
This subcommand is meant to be used against OpenShift clusters and serve as a shortcut to trigger well-known workloads

Usage:
  kube-burner ocp [command]

Available Commands:
  cluster-density-ms             Runs cluster-density-ms workload
  cluster-density-v2             Runs cluster-density-v2 workload
  crd-scale                      Runs crd-scale workload
  index                          Runs index sub-command
  networkpolicy-matchexpressions Runs networkpolicy-matchexpressions workload
  networkpolicy-matchlabels      Runs networkpolicy-matchlabels workload
  networkpolicy-multitenant      Runs networkpolicy-multitenant workload
  node-density                   Runs node-density workload
  node-density-cni               Runs node-density-cni workload
  node-density-heavy             Runs node-density-heavy workload
  pvc-density                    Runs pvc-density workload
  web-burner-cluster-density     Runs web-burner-cluster-density workload
  web-burner-init                Runs web-burner-init workload
  web-burner-node-density        Runs web-burner-node-density workload

Flags:
      --alerting                  Enable alerting (default true)
      --burst int                 Burst (default 20)
      --es-index string           Elastic Search index
      --es-server string          Elastic Search endpoint
      --extract                   Extract workload in the current directory
      --gc                        Garbage collect created namespaces (default true)
      --gc-metrics                Collect metrics during garbage collection
  -h, --help                      help for ocp
      --local-indexing            Enable local indexing
      --metrics-endpoint string   YAML file with a list of metric endpoints
      --profile-type string       Metrics profile to use, supported options are: regular, reporting or both (default "both")
      --qps int                   QPS (default 20)
      --timeout duration          Benchmark timeout (default 4h0m0s)
      --user-metadata string      User provided metadata file, in YAML format
      --uuid string               Benchmark UUID (default "9e14107b-3d15-4904-843b-eac6d23ebd40")


Global Flags:
      --log-level string   Allowed values: debug, info, warn, error, fatal (default "info")

Use "kube-burner ocp [command] --help" for more information about a command.
```

## Usage

Some of the benefits the OCP wrapper provides are:

- Simplified execution of the supported workloads. (Only some flags are required)
- Indexes OpenShift metadata along with the Benchmark result. This document can be found with the following query: `uuid: <benchmkark-uuid> AND metricName.keyword: "clusterMetadata"`
- Prevents modifying configuration files to tweak some of the parameters of the workloads.
- Discovers the Prometheus URL and authentication token, so the user does not have to perform those operations before using them.

In order to trigger one of the supported workloads using this subcommand, you must run kube-burner using the subcommand `ocp`. The workloads are embedded in the kube-burner binary:

Running node-density with 100 pods per node

```console
kube-burner ocp node-density --pods-per-node=100
```

With the command above, the wrapper will calculate the required number of pods to deploy across all worker nodes of the cluster.

## Multiple endpoints support

The flag `--metrics-endpoint` can be used to interact with multiple Prometheus endpoints
For example:

```console
kube-burner ocp cluster-density-v2 --iterations=1 --churn-duration=2m0s --es-index kube-burner --es-server https://www.esurl.com:443 --metrics-endpoint metrics-endpoints.yaml
```

## Cluster density workloads

This workload family is a control-plane density focused workload that that creates different objects across the cluster. There are 2 different variants [cluster-density-v2](#cluster-density-v2) and [cluster-density-ms](#cluster-density-ms).

Each iteration of these create a new namespace, the three support similar configuration flags. Check them out from the subcommand help.

!!! Info
    Workload churning of 1h is enabled by default in the `cluster-density` workloads; you can disable it by passing `--churn=false` to the workload subcommand.

### cluster-density-v2

Each iteration creates the following objects in each of the created namespaces:

- 1 image stream.
- 1 build. The OCP internal container registry must be set-up previously because the resulting container image will be pushed there.
- 3 deployments with two pod 2 replicas (nginx) mounting 4 secrets, 4 config maps, and 1 downward API volume each.
- 2 deployments with two pod 2 replicas (curl) mounting 4 Secrets, 4 config maps and 1 downward API volume each. These pods have configured a readiness probe that makes a request to one of the services and one of the routes created by this workload every 10 seconds.
- 5 services, each one pointing to the TCP/8080 port of one of the nginx deployments.
- 2 edge routes pointing to the to first and second services respectively.
- 10 secrets containing a 2048-character random string.
- 10 config maps containing a 2048-character random string.
- 3 network policies:
    - deny-all traffic
    - allow traffic from client/nginx pods to server/nginx pods
    - allow traffic from openshift-ingress namespace (where routers are deployed by default) to the namespace

### cluster-density-ms

Lightest version of this workload family, each iteration the following objects in each of the created namespaces:

- 1 image stream.
- 4 deployments with two pod replicas (pause) mounting 4 secrets, 4 config maps, and 1 downward API volume each.
- 2 services, each one pointing to the TCP/8080 and TCP/8443 ports of the first and second deployment respectively.
- 1 edge route pointing to the to first service.
- 20 secrets containing a 2048-character random string.
- 10 config maps containing a 2048-character random string.

## Node density workloads

The workloads of this family create a single namespace with a set of pods, deployments, and services depending on the workload.

### node-density

This workload is meant to fill with pause pods all the worker nodes from the cluster. It can be customized with the following flags. This workload is usually used to measure the Pod's ready latency KPI.

### node-density-cni

It creates two deployments, a client/curl and a server/nxing, and 1 service backed by the previous server pods. The client application has configured an startup probe that makes requests to the previous service every second with a timeout of 600s.

Note: This workload calculates the number of iterations to create from the number of nodes and desired pods per node.  In order to keep the test scalable and performant, chunks of 1000 iterations will by broken into separate namespaces, using the config variable `iterationsPerNamespace`.

### node-density-heavy

Creates two deployments, a postgresql database, and a simple client that performs periodic insert queries (configured through liveness and readiness probes) on the previous database and a service that is used by the client to reach the database.

Note: this workload calculates the number of iterations to create from the number of nodes and desired pods per node.  In order to keep the test scalable and performant, chunks of 1000 iterations will by broken into separate namespaces, using the config variable `iterationsPerNamespace`.

## Network Policy workloads

With the help of [networkpolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/) object we can control traffic flow at the IP address or port level in Kubernetes. A networkpolicy can come in various shapes and sizes. Allow traffic from a specific namespace, Deny traffic from a specific pod IP, Deny all traffic, etc. Hence we have come up with a few test cases which try to cover most of them. They are as follows.

### networkpolicy-multitenant

- 500 namespaces
- 20 pods in each namespace. Each pod acts as a server and a client
- Default deny networkpolicy is applied first that blocks traffic to any test namespace
- 3 network policies in each namespace that allows traffic from the same namespace and two other namespaces using namespace selectors

### networkpolicy-matchlabels

- 5 namespaces
- 100 pods in each namespace. Each pod acts as a server and a client
- Each pod with 2 labels and each label shared is by 5 pods
- Default deny networkpolicy is applied first
- Then for each unique label in a namespace we have a networkpolicy with that label as a podSelector which allows traffic from pods with some other randomly selected label. This translates to 40 networkpolicies/namespace

### networkpolicy-matchexpressions

- 5 namespaces
- 25 pods in each namespace. Each pod acts as a server and a client
- Each pod with 2 labels and each label shared is by 5 pods
- Default deny networkpolicy is applied first
- Then for each unique label in a namespace we have a networkpolicy with that label as a podSelector which allows traffic from pods which *don't* have some other randomly-selected label. This translates to 10 networkpolicies/namespace

## Web-burner workloads
This workload is meant to emulate some telco specific workloads. Before running *web-burner-node-density* or *web-burner-cluster-density* load the environment with *web-burner-init* first (without the garbage collection flag: `--gc=false`).

Pre-requisites:
 - At least two worker nodes
 - At least one of the worker nodes must have the `node-role.kubernetes.io/worker-spk` label

### web-burner-init

- 35 (macvlan/sriov) networks for 35 lb namespace
- 35 lb-ns
  - 1 frr config map, 4 emulated lb pods on each namespace
-  35 app-ns
	- 1 emulated lb pod on each namespace for bfd session

### web-burner-node-density
- 35 app-ns
  - 3 app pods and services on each namespace
- 35 normal-ns
	- 1 service with 60 normal pod endpoints on each namespace

### web-burner-cluster-density
- 20 normal-ns
	- 30 configmaps, 38 secrets, 38 normal pods and services, 5 deployments with 2 replica pods on each namespace
- 35 served-ns
  - 3 app pods on each namespace
- 2 app-served-ns
	- 1 service(15 ports) with 84 pod endpoints, 1 service(15 ports) with 56 pod endpoints, 1 service(15 ports) with 25 pod endpoints
	- 3 service(15 ports each) with 24 pod endpoints, 3 service(15 ports each) with 14 pod endpoints
	- 6 service(15 ports each) with 12 pod endpoints, 6 service(15 ports each) with 10 pod endpoints, 6 service(15 ports each) with 9 pod endpoints
	- 12 service(15 ports each) with 8 pod endpoints, 12 service(15 ports each) with 6 pod endpoints, 12 service(15 ports each) with 5 pod endpoints
	- 29 service(15 ports each) with 4 pod endpoints, 29 service(15 ports each) with 6 pod endpoints

## Index

Just like the traditional kube-burner, ocp wrapper also has an indexing functionality which is exposed as `index` subcommand.

```console
$ kube-burner ocp index --help
If no other indexer is specified, local indexer is used by default

Usage:
  kube-burner ocp index [flags]

Flags:
  -m, --metrics-profile string     Metrics profile file (default "metrics.yml")
      --metrics-directory string   Directory to dump the metrics files in, when using default local indexing (default "collected-metrics")
  -s, --step duration              Prometheus step size (default 30s)
      --start int                  Epoch start time
      --end int                    Epoch end time
  -j, --job-name string            Indexing job name (default "kube-burner-ocp-indexing")
      --user-metadata string       User provided metadata file, in YAML format
  -h, --help                       help for index
```

Please refer to [indexing](observability/indexing.md) section for better understanding on the functionality.

## Metrics-profile type

By specifying `--profile-type`, kube-burner can use two different metrics profiles when scraping metrics from prometheus. By default is configured with `both`, meaning that it will use the regular metrics profiles bound to the workload in question and the reporting metrics profile.

When using the regular profiles ([metrics-aggregated](https://github.com/kube-burner/kube-burner/blob/master/cmd/kube-burner/ocp-config/metrics-aggregated.yml) or [metrics](https://github.com/kube-burner/kube-burner/blob/master/cmd/kube-burner/ocp-config/metrics.yml)), kube-burner scrapes and indexes metrics timeseries.

The reporting profile is very useful to reduce the number of documents sent to the configured indexer. Thanks to the combination of aggregations and instant queries for prometheus metrics, and 4 summaries for latency measurements, only a few documents will be indexed per benchmark. This flag makes possible to specify one or both of these profiles indistinctly.

## Customizing workloads

It is possible to customize any of the above workload configurations by extracting, updating, and finally running it:

```console
$ kube-burner ocp node-density --extract
$ ls
alerts.yml  metrics.yml  node-density.yml  pod.yml  metrics-report.yml
$ vi node-density.yml                               # Perform modifications accordingly
$ kube-burner ocp node-density --pods-per-node=100  # Run workload
```

## Cluster metadata

When the benchmark finishes, kube-burner will index the cluster metadata in the configured indexer. Currently. this is based on the following Golang struct:

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

Where `ocpmetadata.ClusterMetadata` is an embed struct inherited from the [go-commons library](https://github.com/kube-burner/go-commons/blob/main/ocp-metadata/types.go), which has the following fields:

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
  ExecutionErrors  string `json:"executionErrors"`
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
