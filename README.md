[![Build Status](https://github.com/cloud-bulldozer/kube-burner/workflows/Go/badge.svg?branch=master)](https://github.com/cloud-bulldozer/kube-burner/actions?query=workflow%3AGo)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloud-bulldozer/kube-burner)](https://goreportcard.com/report/github.com/cloud-bulldozer/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

<img src="./media/logo/kube-burner-logo.png" width="60%">

---
- [What's this?](#whats-this)
- [Quick start](#quick-start)
- [Building](#building)
- [Getting started](#getting-started)
- [Configuration](#configuration)
  - [Objects](#objects)
  - [Job types](#job-types)
  - [Injected variables](#injected-variables)
  - [Metrics profile](#metrics-profile)
  - [Indexers](#indexers)
  - [Measurements](#measurements)
    - [Pod latency](#pod-latency)
    - [Pprof collection](#pprof-collection)
- [Contributing to kube-burner](#contributing-to-kube-burner)
  - [Requirements](#requirements)


## What's this?

Kube-burner is a tool aimed to stress a kubernetes cluster. An overview of its behaviour can be summarized with these three steps:

- Create the objects declared in the jobs.
- Collect desired on-cluster prometheus metrics.
- Write and/or index them to the configured TSDB.

[![asciicast](https://asciinema.org/a/KksoK5voK3al1FuOza89t1JAp.svg)](https://asciinema.org/a/KksoK5voK3al1FuOza89t1JAp)


## Quick start

In case you want to start tinkering with `kube-burner` now:

- You can find the binaries in the [releases section of this repository](https://github.com/cloud-bulldozer/kube-burner/releases).
- There's also a container image available at [quay](https://quay.io/repository/cloud-bulldozer/kube-burner?tab=tags).
- A valid example of a configuration file can be found at [./examples/cfg.yml](./examples/cfg.yml)


## Building

To build kube-burner just execute `make build`, once finished `kube-burner`'s binary should be available at `./bin/kube-burner`

```console
$ make build
building kube-burner 0.1.0
GOPATH=/home/rsevilla/go
CGO_ENABLED=0 go build -v -mod vendor -ldflags "-X github.com/cloud-bulldozer/kube-burner/version.GitCommit=d91c8cc35cb458a4b80a5050704a51c7c6e35076 -X github.com/cloud-bulldozer/kube-burner/version.BuildDate=2020-08-19-19:10:09 -X github.com/cloud-bulldozer/kube-burner/version.GitBranch=master" -o bin/kube-burner
```

## Getting started

kube-burner is basically a binary client with currently the following options.

```console
./bin/kube-burner help
kube-burner is a tool that aims to stress a kubernetes cluster.

It doesn’t only provide similar features as other tools like cluster-loader, but also
adds other features such as simplified simplified usage, metrics collection and indexing capabilities

Usage:
  kube-burner [command]

Available Commands:
  completion  Generates completion scripts for bash shell
  destroy     Destroy old namespaces labeled with the given UUID.
  help        Help about any command
  index       Index metrics from the given time range
  init        Launch benchmark
  version     Print the version number of kube-burner

Flags:
  -h, --help   help for kube-burner

Use "kube-burner [command] --help" for more information about a command.

```

- The **init** option supports the following flags:
  - config: Path to a valid configuration file.
  - log-level: Logging level. Default `info`
  - prometheus-url: Prometheus full URL. i.e. `https://prometheus-k8s-openshift-monitoring.apps.rsevilla.stress.mycluster.example.com`
  - metrics-profile: Path to a valid metrics profile file. Default `metrics.yaml`
  - token: Prometheus Bearer token.
  - username: Prometheus username for basic authentication.
  - password: Prometheus password for basic authentication.
  - skip-tls-verify: Skip TLS verification for prometheus. Default `true`
  - step: Prometheus step size. Default `30s`
  - UUID: Benchmark UUID.

**Note**: Both basic authentication and Bearer authentication need credentials able to query given Prometheus API.

With the above, triggering kube-burner would be as simple as:

```console
$ kube-burner init -c cfg.yml -u https://prometheus-k8s-openshift-monitoring.apps.rsevilla.stress.mycluster.example.com -t ${token} --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

If you have no interest in collecting prometheus metrics, kube-burner can also be launched w/o any prometheus endpoint.

```console
$ kube-burner init -c cfg.yml --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

- The **index** option can be used to collect and index the metrics from a given time range. This option supports the same flags as the **init** option. The time range is given by:
  - start: Epoch start time. Defaults to one hour before the current time.
  - End: Epoch end time. Defaults to the current time.

- The **destroy** option requires the above `config` and `UUID` flags to destroy all namespaces labeled with `kube-burner-uuid=<UUID>`.

- The **completion** option generates bash a completion script that can be imported with:
  - `. <(kube-burner completion)` 
  - `kube-burner completion > /etc/bash_completion.d/kube-burner`

## Configuration

All the magic `kube-burner` does is described in the configuration file. This file is written in YAML format and has the following sections:

* global: This section describes the global job configuration, it holds the following parameters:

| Option           | Description                                                                                              | Type           | Example        | Default     |
|------------------|----------------------------------------------------------------------------------------------------------|----------------|----------------|-------------|
| kubeconfig       | Points to a valid kubeconfig file. Can be omitted if using the KUBECONFIG environment variable | String  | ~/mykubeconfig | in-cluster |             |
| writeToFile      | Whether to dump collected metrics to files                                                               | Boolean        | true           | true        |
| metricsDirectory | Directory where collected metrics will be dumped into. It will be created if it doesn't exist previously | String         | ./metrics      | ./collected-metrics | 
| measurements     | List of measurements. Detailed in the [measurements section](#Measurements)                              | List           | -              | []          |
| indexerConfig    | Holds the indexer configuration. Detailed in the [indexers section](#Indexers)                           | Object         | -              | -           |

* jobs: This section contains a list of jobs that `kube-burner` will execute. Each job can hold the following parameters.

| Option               | Description                                                                      | Type    | Example  | Default |
|----------------------|----------------------------------------------------------------------------------|---------|----------|---------|
| name                 | Job name                                                                         | String  | myjob    | ""      |
| jobType              | Type of job to execute. More details at [job types](#job-types)                  | string  | create   | create  |
| jobIterations        | How many times to execute the job                                                | Integer | 10       | 0       |
| namespace            | Namespace base name to use                                                       | String  | firstjob | ""      |
| namespacedIterations | Whether to create a namespace per job iteration                                  | Boolean | true     | true    |
| cleanup              | Cleanup clean up old namespaces                                                  | Boolean | true     | true    |
| podWait              | Wait for all pods to be running before moving forward to the next job iteration  | Boolean | true     | true    |
| waitWhenFinished     | Wait for all pods to be running when all iterations are completed                | Boolean | true     | false   |
| maxWaitTimeout       | Maximum wait timeout in seconds. (If podWait is enabled this timeout will be reseted with each iteration) | Integer | 1h     | 12h |
| waitFor              | List containing the objects Kind wait for. Wait for all if empty                 | List    | ["Deployment", "Build", "DaemonSet"]| []      |
| jobIterationDelay    | How long to wait between each job iteration                                      | Duration| 2s       | 0s      |
| jobPause             | How long to pause after finishing the job                                        | Duration| 10s      | 0s      |
| qps                  | Limit object creation queries per second                                         | Integer | 25       | 0       |
| burst                | Maximum burst for throttle                                                       | Integer | 50       | 0       |
| objects              | List of objects the job will create. Detailed on the [objects section](#objects) | List    | -        | []      |
| verifyObjects        | Verify object count after running each job. Return code will be 1 if failed      | Boolean | true     | true    |
| errorOnVerify        | Exit with rc 1 before indexing when objects verification fails                   | Boolean | true     | false   |


A valid example of a configuration file can be found at [./examples/cfg.yml](./examples/cfg.yml)

### Objects

The objects created by `kube-burner` are rendered using the default golang's [template library](https://golang.org/pkg/text/template/).
Each object element supports the following parameters:

| Option               | Description                                                       | Type    | Example        | Default |
|----------------------|-------------------------------------------------------------------|---------|----------------|---------|
| objectTemplate       | Object template file                                              | String  | deployment.yml | ""      |
| replicas             | How replicas of this object to create per job iteration           | Integer | 10             | -       |
| inputVars            | Map of arbitrary input variables to inject to the object template | Object  | -              | -       |

It's important to note that all objects created by kube-burner are labeled with. `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`


### Job types

kube-burner support two types of jobs with different parameters each. The default job type is __create__. Which basically creates objects as described in the section [objects](#objects).

The other type is __delete__, this type of job deletes objects described in the objects list. Using delete as job type the objects list would have the following structure.

```yaml
objects:
- kind: Deployment
  labelSelector: {kube-burner-job: cluster-density}
  apiVersion: apps/v1

- kind: Secret
  labelSelector: {kube-burner-job: cluster-density}
```
Where:
- kind: Object kind of the k8s object to delete.
- labelSelector: Map with the labelSelector.
- apiVersion: API version from the k8s object.

As mentioned previously, all objects created by kube-burner are labeled with `kube-burner-uuid=<UUID>,kube-burner-job=<jobName>,kube-burner-index=<objectIndex>`. Thanks to this we could design a workload with one job to create objects and another one able to remove the objects created by the previous

```yaml
jobs:
- name: create-objects
  namespace: job-namespace
  jobIterations: 100
  objects:
  - objectTemplate: deployment.yml
    replicas: 10

  - objectTemplate: service.yml
    replicas: 10

- name: remove-objects
  jobType: delete
  objects:
  - kind: Deployment
    labelSelector: {kube-burner-job: create-objects}
    apiVersion: apps/v1

  - kind: Secret
    labelSelector: {kube-burner-job: create-objects}
```

This job type supports the some of the same parameters as the create job type:
- **waitForDeletion**: Wait for objects to be deleted before finishing the job. Defaults to true
- name
- qps
- burst
- jobPause

### Injected variables

All object templates are injected a series of variables by default:

- Iteration: Job iteration number.
- Replica: Object replica number. Keep in mind that this number is reset to 1 with each job iteration.
- JobName: Job name.
- UUID: Benchmark UUID.

In addition, you can also inject arbitrary variables with the option inputVars from the objectTemplate object:

```yaml
    - objectTemplate: service.yml
      replicas: 2
      inputVars:
        port: 80
        targetPort: 8080
```

The following code snippet shows an example of a k8s service using these variables:


```yaml
apiVersion: v1
kind: Service
metadata:
  name: sleep-app-{{ .Iteration }}-{{ .Replica }}
  labels:
    name: my-app-{{ .Iteration }}-{{ .Replica }}
spec:
  selector:
    app: sleep-app-{{ .Iteration }}-{{ .Replica }}
  ports:
  - name: serviceport
    protocol: TCP
    port: "{{ .port }}"
    targetPort: "{{ .targetPort }}"
  type: ClusterIP
```

It's worth to say that you can also more advanced [golang template semantics](https://golang.org/pkg/text/template/) on your objectTemplate files.

```yaml
kind: ImageStream
apiVersion: image.openshift.io/v1
metadata:
  name: {{.prefix}}-{{.Replica}}
spec:
{{ if .image }}
  dockerImageRepository: {{.image}}
{{ end }}
```

### Metrics profile

The metrics-profile flag points to a YAML file containing a list of the prometheus queries kube-burner will collect for each job.
As soon one of job finishes, `kube-burner` makes a range query for each query described in this file, and indexes it in the index configured by the parameter `defaultIndex`.
We can use the parameter `indexName` in a metrics-profile file to make `kube-burner` to index the resulting metrics to a different index.
An example of a valid metrics profile file is shown below:
The parameter **metricName** is added to the indexed documents, it will allow us to identify documents from a certain query more easily.

```yaml
metrics:
  - query: irate(process_cpu_seconds_total{job=~".*(crio|etcd|controller-manager|apiserver|scheduler).*"}[2m])
    metricName: controlPlaneCPU

  - query: process_resident_memory_bytes{job=~".*(crio|etcd|controller-manager|apiserver|scheduler).*"}
    metricName: controlPlaneMemory

  - query: sum(irate(node_cpu_seconds_total[2m])) by (mode,instance)
    metricName: nodeCPU
```

### Indexers

`kube-burner` is able to **index the collected prometheus metrics** into a given Indexer. 
The indexer configuration is described in the `indexerConfig` section and can be configured with the following parameters:


| Option               | Description           | Type     | Example    | Default |
|----------------------|-----------------------|----------|------------|---------|
| enabled              | Enable indexing       | Boolean  | true       | false   |
| type                 | Type of indexer       | String   | elastic    | ""      | 


The following indexers are currently supported:

- `elastic`: Index documents in Elasticsearch 7 instances.

In addition, each indexer has its own configuration parameters.

----

The `elastic` indexer is configured by the parameters below:

| Option               | Description                                       | Type        | Example                                  | Default |
|----------------------|---------------------------------------------------|-------------|------------------------------------------|---------|
| esServers            | List of ES instances                              | List        | [https://elastic.apps.rsevilla.org:9200] | ""      |
| defaultIndex         | Default index to send the prometheus metrics into | String      | kube-burner                              | ""      | 
| insecureSkipVerify   | TLS certificate verification                      | Boolean     | true                                     | false   |

**Note**: It's possible to index documents in an authenticated ES instance using the notation `http(s)://[username]:[password]@[address]:[port]` in the *esServers* parameter.

### Measurements

Apart from prometheus metrics collection, `kube-burner` allows to get further metrics using other mechanisms or data sources such as the 
own kubernetes API, these mechanisms are called measurements.
Measurements are enabled in the measurements section of the configuration file. This section contains a list of measurements with their options.
'kube-burner' supports the following measurements so far:

#### Pod latency

Collects latencies from the different pod startup phases, these **latency metrics are in ms**. Can be enabled with:

```yaml
    measurements:
    - name: podLatency
      esIndex: kube-burner-podlatency
```

This measurement sends its metrics to the index configured by *esIndex*. The metrics collected are pod latency histograms and pod latency quantiles P99, P95 and P50.

Pod latency sample:
```json
{
    "uuid": "363073c1-5752-4a36-8e0a-1311fa7663f8",
    "timestamp": "2020-08-24T19:05:49.316913942+02:00",
    "schedulingLatency": 8,
    "initializedLatency": 82,
    "containersReadyLatency": 82,
    "podReadyLatency": 82,
    "metricName": "podLatencyMeasurement"
}
```

Pod latency quantile sample:

```json
{
    "quantileName": "initialized",
    "uuid": "363073c1-5752-4a36-8e0a-1311fa7663f8",
    "P99": 76543,
    "P95": 72702,
    "P50": 336,
    "Max": 84523,
    "Avg": 53131,
    "timestamp": "2020-08-27T01:13:24.091110065+02:00",
    "metricName": "podLatencyQuantilesMeasurement"
},
{
    "quantileName": "podReady",
    "uuid": "363073c1-5752-4a36-8e0a-1311fa7663f8",
    "P99": 76543,
    "P95": 72702,
    "P50": 336,
    "Max": 82522,
    "Avg": 54153,
    "timestamp": "2020-08-27T01:13:24.091110483+02:00",
    "metricName": "podLatencyQuantilesMeasurement"
}
```

The __esIndex__ option can be used to configure the ES index where metrics will be indexed.

#### Pprof collection

This measurement takes care of collecting golang profiling information from pods. To do so, kube-burner connects to pods with the given labels running in certain namespaces. This measurement uses an implementation similar to `kubectl exec`, and as soon as it connects to one pod it executes the command `curl <pprofURL>` to get the pprof data. Pprof files are collected in a regular basis given by the parameter `pprofInterval` and these files are stored in the directory configured by the parameter `pprofDirectory` which by default is `pprof`.
It's also possible to configure a token to get pprof data from authenticated endoints such as kube-apiserver with the variable `bearerToken`.

An example of how to configure this measurement to collect pprof HEAP and CPU profiling data from kube-apiserver is shown below:

```yaml
   measurements:
    - name: pprof
      pprofInterval: 5m
      pprofDirectory: pprof-data
      pprofTargets:
        - name: kube-apiserver-heap
          namespace: "openshift-kube-apiserver"
          labelSelector: {app: openshift-kube-apiserver}
          bearerToken: thisIsNotAValidToken
          url: https://localhost:6443/debug/pprof/heap

        - name: kube-apiserver-cpu
          namespace: "openshift-kube-apiserver"
          labelSelector: {app: openshift-kube-apiserver}
          bearerToken: thisIsNotAValidToken
          url: https://localhost:6443/debug/pprof/profile?timeout=30
```

**Note**: As mentioned before, this measurement requires cURL to be installed in the target pods.

## Contributing to kube-burner

If you want to contribute to kube-burner, submit a Pull Request or Issue.

### Requirements

- `golang >= 1.13`
- `make`
