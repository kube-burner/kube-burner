# Measurements

Kube-burner allows to get further metrics using other mechanisms or data sources such as the own kubernetes API, these mechanisms are called measurements.

Measurements are enabled in the measurements section of the configuration file. This section contains a list of measurements with their options.
'kube-burner' supports the following measurements so far:

!!! Warning
    `podLatency`, as any other measurement, is only captured during a benchmark runtime. It does not work with the `index` subcommand of kube-burner

## Pod latency

Collects latencies from the different pod startup phases, these **latency metrics are in ms**. It can be enabled with:

```yaml
  measurements:
  - name: podLatency
```

This measurement sends its metrics to configured indexer. The metrics collected are pod latency histograms (`podLatencyMeasurement`) and a four documents holding a summary with different pod latency quantiles of each pod condition (`podLatencyQuantilesMeasurement`). It's possible to skip indexing the `podLatencyMeasurement` metric by configuring the field `podLatencyMetrics` of this measurement to `quantiles`.

Pod latency sample, one document like the below is indexed per each pod created by the workload that enters in Running condition during the workload:

```json
{
  "timestamp": "2020-11-15T20:28:59.598727718Z",
  "schedulingLatency": 4,
  "initializedLatency": 20,
  "containersReadyLatency": 2997,
  "podReadyLatency": 2997,
  "metricName": "podLatencyMeasurement",
  "jobName": "kubelet-density",
  "uuid": "c40b4346-7af7-4c63-9ab4-aae7ccdd0616",
  "namespace": "kubelet-density",
  "podName": "kubelet-density-13",
  "jobName": "kube-burner-job",
  "nodeName": "worker-001"
}
```

---

Pod latency quantile sample:

```json
{
  "quantileName": "Ready",
  "uuid": "23c0b5fd-c17e-4326-a389-b3aebc774c82",
  "P99": 3774,
  "P95": 3510,
  "P50": 2897,
  "max": 3774,
  "avg": 2876.3,
  "timestamp": "2020-11-15T22:26:51.553221077+01:00",
  "metricName": "podLatencyQuantilesMeasurement",
  "jobName": "kubelet-density"
},
{
  "quantileName": "PodScheduled",
  "uuid": "23c0b5fd-c17e-4326-a389-b3aebc774c82",
  "P99": 64,
  "P95": 8,
  "P50": 5,
  "max": 64,
  "avg": 5.38,
  "timestamp": "2020-11-15T22:26:51.553225151+01:00",
  "metricName": "podLatencyQuantilesMeasurement",
  "jobName": "kubelet-density"
}
```

Where quantileName matches with the pod conditions and can be:

- `PodScheduled`: Pod has been scheduled in to a node.
- `Initialized`: All init containers in the pod have started successfully
- `ContainersReady`: Indicates whether all containers in the pod are ready.
- `Ready`: The pod is able to service reqeusts and should be added to the load balancing pools of all matching services.

!!! info
    More information about the pod conditions can be found at the [kubernetes documentation site](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions).

And the metrics are:

- `P99`: 99th percentile of the pod condition.
- `P95`: 95th percentile of the pod condition.
- `P50`: 50th percentile of the pod condition.
- `Max`: Maximum value of the condition.
- `Avg`: Average value of the condition.

### Pod latency thresholds

It's possible to establish pod latency thresholds to the different pod conditions and metrics by defining the option `thresholds` within this measurement:

Establishing a threshold of 2000ms in the P99 metric of the `Ready` condition.

```yaml
  measurements:
  - name: podLatency
    thresholds:
    - conditionType: Ready
      metric: P99
      threshold: 2000ms
```

Latency thresholds are evaluated at the end of each job, showing an informative message like the following:

```console
INFO[2020-12-15 12:37:08] Evaluating latency thresholds
WARN[2020-12-15 12:37:08] P99 Ready latency (2929ms) higher than configured threshold: 2000ms
```

In case of not meeting any of the configured thresholds, like the example above, **kube-burner return code will be 1**.

## pprof collection

This measurement can be used to collect golang profiling information from processes running in pods from the cluster. To do so, kube-burner connects to pods labeled with `labelSelector` and running in `namespace`. This measurement uses an implementation similar to `kubectl exec`, and as soon as it connects to one pod it executes the command `curl <pprofURL>` to get the pprof data. pprof files are collected in a regular basis configured by the parameter `pprofInterval`, the collected pprof files are downloaded from the pods to the local directory configured by the parameter `pprofDirectory` which by default is `pprof`.

As some components require authentication to get profiling information, `kube-burner` provides two different modalities to address it:

- **Bearer token authentication**: This modality is configured by the variable `bearerToken`, which holds a valid Bearer token that will be used by cURL to get pprof data. This method is usually valid with kube-apiserver and kube-controller-managers components
- **Certificate Authentication**: Usually valid for etcd, this method can be configured using a combination of cert/privKey files or directly using the cert/privkey content, it can be tweaked with the following variables:
    - `cert`: Base64 encoded certificate.
    - `key`: Base64 encoded private key.
    - `certFile`: Path to a certificate file.
    - `keyFile`: Path to a private key file.

!!! note
    The decoded content of the certificate and private key is written to the files /tmp/pprof.crt and /tmp/pprof.key of the remote pods respectively

An example of how to configure this measurement to collect pprof HEAP and CPU profiling data from kube-apiserver and etcd is shown below:

```yaml
  measurements:
  - name: pprof
    pprofInterval: 30m
    pprofDirectory: pprof-data
    pprofTargets:
    - name: kube-apiserver-heap
      namespace: "openshift-kube-apiserver"
      labelSelector: {app: openshift-kube-apiserver}
      bearerToken: thisIsNotAValidToken
      url: https://localhost:6443/debug/pprof/heap

    - name: etcd-heap
      namespace: "openshift-etcd"
      labelSelector: {app: etcd}
      certFile: etcd-peer-pert.crt
      keyFile: etcd-peer-pert.key
      url: https://localhost:2379/debug/pprof/heap
```

!!! warning
    As mentioned before, this measurement requires the `curl` command to be available in the target pods.
