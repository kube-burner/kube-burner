# Measurements

Apart from prometheus metrics collection, Kube-burner allows to get further metrics using other mechanisms or data sources such as the own kubernetes API, these mechanisms are called measurements.

Measurements are enabled in the measurements section of the configuration file. This section contains a list of measurements with their options.
'kube-burner' supports the following measurements so far:

**Note:** podLatency measurement is only captured when a benchmark is triggered. It does not work with the "index" mode of kube-burner

## Pod latency

Collects latencies from the different pod startup phases, these **latency metrics are in ms**. Can be enabled with:

```yaml
  measurements:
  - name: podLatency
```

This measurement sends its metrics to configured indexer. The metrics collected are pod latency histograms and pod latency quantiles P99, P95 and P50.

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

Where quantileName matches with pod conditions and can be:

- __PodScheduled__: Pod has been scheduled in to a node.
- __Initialized__: All init containers in the pod have started successfully
- __ContainersReady__: Indicates whether all containers in the pod are ready.
- __Ready__: The pod is able to service reqeusts and should be added to the load balancing pools of all matching services.

And the metrics are:

- P99: 99th percentile of the pod condition.
- P95: 95th percentile of the pod condition.
- P50: 50th percentile of the pod condition.
- Max: Maximum value of the condition.
- Avg: Average value of the condition.

More information about the pod conditions can be found in the [kubernetes docs site](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions).

### Pod latency thresholds

It's possible to stablish pod latency thresholds in the different pod conditions and metrics through the option `thresholds` from the podLatency measurement:

For example, the example below establish a threshold of 2000ms in the P99 metric of the `Ready` condition.

```yaml
  measurements:
  - name: podLatency
    thresholds:
    - conditionType: Ready
      metric: P99
      thrshold: 2000ms
```

Latency thresholds are evaluated at the end of each job, showing an informative message like the following:

```log
INFO[2020-12-15 12:37:08] Evaluating latency thresholds
WARN[2020-12-15 12:37:08] P99 Ready latency (2929ms) higher than configured threshold: 2000ms
```

**In case of not meeting any of the configured thresholds, like the example above, Kube-burner return code will be 1**

## Pprof collection

This measurement takes care of collecting golang profiling information from pods. To do so, kube-burner connects to pods with the given labels running in certain namespaces. This measurement uses an implementation similar to `kubectl exec`, and as soon as it connects to one pod it executes the command `curl <pprofURL>` to get the pprof data. Pprof files are collected in a regular basis configured by the parameter `pprofInterval`, the collected pprof files are downloaded from the pods to the local directory configured by the parameter `pprofDirectory` which by default is `pprof`.

As some components require authentication to get profiling information, `kube-burner` provides two different methods to address it:

- Bearer token authentication: This modality is configured by the variable `bearerToken`, which holds a valid Bearer token that will be used by cURL to get pprof data. This method is usually valid with kube-apiserver and kube-controller-managers components
- Certificate Authentication, usually valid for etcd: This method can be configured using a combination of cert/privKey files or directly using the cert/privkey content, it can be tweaked with the following variables:
  - cert: Base64 encoded certificate. The decoded content is written to the file `/tmp/pprof.crt` in the remote pods.
  - key: Base64 encoded private key. The decoded content is written to the file `/tmp/pprof.key` in the remote pods.
  - certFile: Path to a certificate file. The content of this file is copied to the remote pods to the path `/tmp/pprof.crt`
  - keyFile: Path to a private key file. The content of this file is copied to the remote pods to the path `/tmp/pprof.key`

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

     - name: kube-apiserver-cpu
       namespace: "openshift-kube-apiserver"
       labelSelector: {app: openshift-kube-apiserver}
       bearerToken: thisIsNotAValidToken
       url: https://localhost:6443/debug/pprof/profile?timeout=30

     - name: etcd-heap
       namespace: "openshift-etcd"
       labelSelector: {app: etcd}
       certFile: etcd-peer-pert.crt
       keyFile: etcd-peer-pert.key
       url: https://localhost:2379/debug/pprof/heap

     - name: etcd-cpu
       namespace: "openshift-etcd"
       labelSelector: {app: etcd}
       cert: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIApyenRsbmdvd0h3WURWUjBqQkIvYXNkZmFkc1NERkRTS0pFWno2MW1OOE1LdXg5eGNpejB3Z2VNR0ExVWRFUVNCCjJ6Q0IySUlVWlhSalpDRndlcmRzdmFkc2MzUmxiUzV6ZG1PQ0ltVjBZMlF1YTNWaVpTMXplWE4wWlcwdWMzWmoKTG1Oc2RYTjBaWEl1Ykc5allXeUNGMlYwWTJRdWIzQmxibk5vYVdaMExXVjBZMlF1YzNaamdpVmxkR05rTG05dwpaVzV6YUdsbWRDMWxkR05rTG5OMll5NWpiSFZ6ZEdWeUxteHZZMkZzZ2dsc2IyTmhiR2h2YzNTQ0F6bzZNWUlNCk1UQXVNQzR4TkRFdU1qSTRnZ2t4TWpjdU1DNHdMakdDQXpvNk1ZY1FBQUFBQUFBQUFBQUFBQUFBQUFBQUFZY0UKQ2dDTjVJY0Vmd0FBQVljUUFBQUZGRkYwOThhc2RmTmtTMjMxMjNBTkJna3Foa2lHOXcwQkFRc0ZBQU9DQVFFQQp2bFZ6a2pPSXF1ajN1UzBReGVpeFhXeUNsOHZBcFlhbkQvTHFVTjdBUlNINSt2UStQdzNxUUptMXRkUDNiN1loCjZOWkMxRHVBL24va3dXT01YdGZzYmFvYlpqVjVYbFBLanA0T3o3U0lGRTRQOFpteVl4VE5Tb3JkbWJrUzRlelMKM1FwSk9GMzRJZ3lxeG9qVGJRa1d5NGwzUFpmcEF0N3lwV3JiUWlYOE45Y1NJS0ZpY0pOK3VBemVERE0ycXNoMwpYeGsyUFF0bW14cGZPZTlWYUFqMWRuR2c5WU5pRjdFQ3VPRDhuWnNjdG14NzlyUGVCT3pJNU4wOTNvcC9lRk5iCnlURTAzV2p5TlA4MnhmaVFuRmZaUXZxWDZuaWFzZGY5ODdBU2ZkNFgxeWgwMHlteHBLL2NHMXM3QlBMU3BhTDEKeENNQmRGOW5EM3RxOXYvb3lLSmp2UT09ICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIAotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==
       key: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLSAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgIApGT092cEFJQkFBS0NBUUVBcnVWMWpiS01xaTVYZkNhejdFeTZCbjVYR1lPSnZZYzRWV0twVDRPWi9UekJhckRPCk9rb3UwRlUwNGtsREswNlpham0xM05LSERaanpoemNza085eEtRdkZEYW5SMTd6cVYwYVgwM01aUjRweWRiVXYKV0xLU0U5S0FLQTgyMzI5OGdua2VROXNvclV3THJ0am5GSGRVRHdmYWdJT2ZmbVBpWlBJRk0xaEFCWjUwQjFGVgp4WENSZGFBQUlHS1l2QWcwWmNhckU0TFhTYXNkZi9hc2RTazMya1ZNZDJ0N3VuQTJuQUljeEptaWs0QWNaeG9rCm9JZ09JSUlJSUlMMUxPVG9BMFhZUHhrdU9jV1RwSWc2WENWUXVJMTA0U0taOUs3eVpTV21FQm0zRTJ1U1d6KysKeC9sWXB3L3ZFamFXZWFLOXFkbUNqWHVkK2pjZWlDTGptaUpGNFFJREFRQUJBb0lCQURndm93bzRlQlFiK3lMNQpWQWZ2eGp0Ynp5TjFMSVRrc2VaTVlkUVhsUnJUcjlkVW9ZdjhWUG04eGRhRWJyMjA3SGhCdmZrSThUWWZFdTAzCmZtdTVZSU10TXNybTZYRURVYzFqOGxhTnZXdE1RT1VycGVBNnpjN3NKaHZTNXlzMDlnTTZIK1J3dmFxZXFZb3MKU0dBOHpaWmVrWVdEdzNOWkoxd0NuRVVZYnNqZUVFMjk4ZmpMZnZzN3ZrNUFzd0QyeXlYMmZGNHdFQkx3TWkwTAotLS0tLUVORCBSU0EgUFJJVkFURSBLRVktLS0tLSAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgCgo=
       url: https://localhost:2379/debug/pprof/profile?timeout=30
       
```

**Note**: As mentioned before, this measurement requires the `curl` command to be available in the target pods.
