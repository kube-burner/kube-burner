# Measurements

Apart from prometheus metrics collection, Kube-burner allows to get further metrics using other mechanisms or data sources such as the own kubernetes API, these mechanisms are called measurements.
Measurements are enabled in the measurements section of the configuration file. This section contains a list of measurements with their options.
'kube-burner' supports the following measurements so far:

## Pod latency

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
  "jobName": "kube-burner-job"
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

Where quantileName matches with pod conditions and can be:
- PodScheduled: Pod has been scheduled in to a node.
- ContainersReady: Indicates whether all containers in the pod are ready.
- Initialized: All init containers in the pod have started successfully
- Ready: The pod is able to service reqeusts and should be added to the load balancing pools of all matching services.

And the metrics are:
- P99: 99th percentile of the pod condition.
- P95: 95th percentile of the pod condition.
- P50: 50th percentile of the pod condition.
- Max: Maximum value of the condition.
- Avg: Average value of the condition.

More information about the pod lifecycle can be found in the [kubernetes docs](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/).

**Note**: The __esIndex__ option can be used to configure the ES index where metrics will be indexed.

### Pod latency thresholds

It's possible to stablish pod latency thresholds in the different pod conditions and metrics through the option `thresholds` from the podLatency measurement:

For example, the example below establish a threshold of 2000ms in the P99 metric of the `Ready` condition.

```yaml
  measurements:
  - name: podLatency
    esIndex: kube-burner-podlatency
    thresholds:
    - conditionType: Ready
      metric: P99
      thrshold: 2000ms
```

Latency thresholds are evaluated at the end of each job, showing an informative message like the following:

```
INFO[2020-12-15 12:37:08] Evaluating latency thresholds
WARN[2020-12-15 12:37:08] P99 Ready latency (2929ms) higher than configured threshold: 2000ms
```

**In case of not meeting any of the configured thresholds, like the example above, Kube-burner return code will be 1**

## Pprof collection

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
