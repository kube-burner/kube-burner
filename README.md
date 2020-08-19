[![Build Status](https://travis-ci.org/rsevilla87/kube-burner.svg?branch=master)](https://travis-ci.org/rsevilla87/kube-burner)
[![Go Report Card](https://goreportcard.com/badge/github.com/rsevilla87/kube-burner)](https://goreportcard.com/report/github.com/rsevilla87/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# kube-burner

Kube-burner is a tool aimed to stress a kubernetes cluster. An overview of its behaviour can be summarized with these three steps:

- Create the objects declared in the jobs.
- Collect desired on-cluster prometheus metrics.
- Write and/or index them to the configured TSDB.

[![asciicast](https://asciinema.org/a/KksoK5voK3al1FuOza89t1JAp.svg)](https://asciinema.org/a/KksoK5voK3al1FuOza89t1JAp)


## Quick start

You can find the binaries at the [releases section](https://github.com/rsevilla87/kube-burner/releases).
There's also a container image available at `quay.io/rsevilla/kube-burner:latest`.
A valid example of a configuration file can be found at [./examples/cfg.yml](./examples/cfg.yml)


## Buiding

To build kube-burner just execute `make build`, once finished `kube-burner`'s binary should be available at `./bin/kube-burner`

```console
$ make build
building kube-burner 0.1.0
GOPATH=/home/rsevilla/go
CGO_ENABLED=0 go build -v -mod vendor -ldflags "-X github.com/rsevilla87/kube-burner/version.GitCommit=d91c8cc35cb458a4b80a5050704a51c7c6e35076 -X github.com/rsevilla87/kube-burner/version.BuildDate=2020-08-19-19:10:09 -X github.com/rsevilla87/kube-burner/version.GitBranch=master" -o bin/kube-burner
```

## Getting started

kube-burner is basically a binary client with currently the following options.

```console
./bin/kube-burner help
INFO[2020-08-11 12:37:30] 🔥 Starting kube-burner                       
kube-burner is a tool that aims to stress a kubernetes cluster.

It doesn’t only provide similar features as other tools like cluster-loader, but also
adds other features such as simplified simplified usage, metrics collection and indexing capabilities

Usage:
  kube-burner [command]

Available Commands:
  completion  Generates completion scripts for the specified bash shell
  destroy     Destroy old namespaces labeled with the given UUID.
  help        Help about any command
  init        Launch benchmark
  version     Print the version number of kube-burner

Flags:
  -h, --help   help for kube-burner

Use "kube-burner [command] --help" for more information about a command.

```

- The **init** option supports the following flags:
  - config: Path to a valid configuration file.
  - log-level: Logging level. Default `info`
  - prometheus-url Prometheus full URL. i.e. `https://  prometheus-k8s-openshift-monitoring.apps.rsevilla.stress.mycluster.example.com`
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

kube-burner can also be launched w/o any prometheus endpoint, so these metrics will not be collected.

```console
$ kube-burner init -c cfg.yml --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

- The **destroy** option requires the above `config` and `UUID` flags to destroy all namespaces labeled with `kube-burner-uuid=<UUID>`.

- The **completion** option generates bash a completion script that can be imported with:
  - `. <(kube-burner completion)` 
  - `kube-burner completion > /etc/bash_completion.d/kube-burner`

## Configuration

All the magic `kube-burner` does is described in the configuration file. This file is written in YAML format and has the following sections:

* global: This section describes the global job configuration, it holds the following parameters:

| Option           | Description                                                                                                   | Type    | Example        | Default     |
|------------------|---------------------------------------------------------------------------------------------------------------|---------|----------------|-------------|
| kubeconfig       | Points to a valid kubeconfig file. This parameter can be omitted if using the KUBECONFIG environment variable | String  | ~/mykubeconfig | -           |
| writeToFile      | Whether to dump collected metrics to files                                                                    | Boolean | true           | false       |
| metricsDirectory | Directory where collected metrics will be dumped into. It will be created if it doesn't exist previously      | String  | ./metrics      | Current dir |
| measurements     | List of measurements. Detailed in the [measurements section](#Measurements)                                   | List    | -              | []          |
| indexerConfig    | Holds the indexer configuration. Detailed in the [indexers section](#Indexers)                                | Object  | -              | -           |

* jobs: This section contains a list of jobs that `kube-burner` will execute. Each job can hold the following parameters.

| Option               | Description                                                                      | Type    | Example  | Default |
|----------------------|----------------------------------------------------------------------------------|---------|----------|---------|
| name                 | Job name                                                                         | String  | myjob    | ""      |
| jobIterations        | How many times to execute the job                                                | Integer | 10       | -       |
| namespace            | Namespace base name to use                                                       | String  | firstjob | ""      |
| namespacedIterations | Whether to create a namespace per job iteration                                  | Boolean | true     | false   |
| cleanup              | Cleanup clean up old namespaces                                                  | Boolean | true     | false   |
| podWait              | Wait for all pods to be running before moving forward to the next job iteration. | Object  | true     | false   |
| jobIterationDelay    | How many milliseconds to wait between each job iteration                         | Integer | 2000     | false   |
| jobPause             | How many milliseconds to pause after finishing the job                           | Integer | 10000    | -       |
| qps                  | Limit object creation queries per second.                                        | Integer | 25       | 0       |
| burst                | Maximum burst for throttle                                                       | Integer | 50       | 0       |
| objects              | List of objects the job will create. Detailed on the [objects section](#objects) | List    | -        | []      |


A valid example of a configuration file can be found at [./examples/cfg.yml](./examples/cfg.yml)

### Objects

The Objects created by `kube-burner` are rendered using the out of the box golang's [template library](https://golang.org/pkg/text/template/)
Each object element supports the following parameters:

| Option               | Description                                                       | Type    | Example        | Default |
|----------------------|-------------------------------------------------------------------|---------|----------------|---------|
| objectTemplate       | Object template file                                              | String  | deployment.yml | ""      |
| replicas             | How replicas of this object to create per job iteration           | Integer | 10             | -       |
| inputVars            | Map of arbitrary input variables to inject to the object template | Object  | -              | -       |

----

## Injected variables
Object templates are injected a series of variables by default:

- Iteration: Job iteration number.
- Replica: Object replica number. Keep in mind that this number is reset to 1 with each job iteration.
- JobName: Job name.
- UUID: Benchmark UUID.

In addition, you can also inject your own custom variables with the option inputVars from the objectTemplate object:

```yaml
    - objectTemplate: service.yml
      replicas: 2
      inputVars:
        port: 80
        targetPort: 8080
```

The following code snippet constains an example of a k8s service using these variables:


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

## Metrics profile

The metrics-profile flag points to a valid YAML file containing a list of the prometheus metrics we want to collect.
When `kube-burner` finishes each job it queries the metrics for the range of time the job lasted and the given step size.
An example of a valid metrics profile file is shown below:

```yaml
metrics:
  - node_memory_Committed_AS_bytes
  - node_memory_MemAvailable_bytes
  - node_memory_MemFree_bytes
  - node_memory_MemTotal_bytes
  - node_cpu_seconds_total
```

The indexed metrics have the following shape:
> $ cat collected-metrics/kube-burner-job-node_memory_Committed_AS_bytes.json
```json
[
  {
    "Timestamp": "2020-08-12T10:02:21.042+02:00",
    "Value": 24482177024,
    "Labels": {
      "__name__": "node_memory_Committed_AS_bytes",
      "endpoint": "https",
      "instance": "ip-10-0-128-40.eu-west-2.compute.internal",
      "job": "node-exporter",
      "namespace": "openshift-monitoring",
      "pod": "node-exporter-jlv24",
      "service": "node-exporter"
    },
    "uuid": "60e952c0-4d23-4266-bbf0-fc11cfe1afaa",
    "jobName": "kube-burner-job1"
  },
  {
    "Timestamp": "2020-08-12T10:02:51.042+02:00",
    "Value": 29052592128,
    "Labels": {
      "__name__": "node_memory_Committed_AS_bytes",
      "endpoint": "https",
      "instance": "ip-10-0-128-40.eu-west-2.compute.internal",
      "job": "node-exporter",
      "namespace": "openshift-monitoring",
      "pod": "node-exporter-jlv24",
      "service": "node-exporter"
    },
    "uuid": "60e952c0-4d23-4266-bbf0-fc11cfe1afaa",
    "jobName": "kube-burner-job1"
  }
]
```


## Indexers

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

| Option               | Description                                    | Type        | Example                                  | Default |
|----------------------|------------------------------------------------|-------------|------------------------------------------|---------|
| esServers            | List of ES instances                           | List        | [https://elastic.apps.rsevilla.org:9200] | ""      |
| index                | Index name to send the prometheus metrics into | String      | kube-burner                              | ""      | 
| username             | Elasticsearch username                         | String      | user                                     | ""      |
| password             | Elasticsearch password                         | String      | secret                                   | ""      |
| insecureSkipVerify   | TLS certificate verification                   | Boolean     | true                                     | false   |


## Measurements

Apart from prometheus metrics collection, `kube-burner` allows to get further metrics using other mechanisms or data sources such as the 
own kubernetes API, these mechanisms are called measurements.
Measurements are enabled by the measurements section of the configuration file. This section contains a list of measurements and their options.
All measurements support the esIndex option that describe the ES index where metrics will be indexed.
'kube-burner' supports the following measurements so far:

* pod-latency: It collects pod startup phases latency metrics. This measurement can be enabled with:

```yaml
    measurements:
    - name: podLatency
      esIndex: kube-burner-podlatency
```

## Contributing to kube-burner
### Requirements

- `golang >= 1.13`
- `make`
