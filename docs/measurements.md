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
  - cert: Certificate string. The content of this string is written to the file `/tmp/pprof.crt` in the remote pods.
  - key: Privete key string. The content of this string is written to the file `/tmp/pprof.key` in the remote pods.
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
       cert: |
         -----BEGIN CERTIFICATE-----
         rztlngowHwYDVR0jBB/asdfadsSDFDSKJEZz61mN8MKux9xciz0wgeMGA1UdEQSB
         2zCB2IIUZXRjZCFwerdsvadsc3RlbS5zdmOCImV0Y2Qua3ViZS1zeXN0ZW0uc3Zj
         LmNsdXN0ZXIubG9jYWyCF2V0Y2Qub3BlbnNoaWZ0LWV0Y2Quc3ZjgiVldGNkLm9w
         ZW5zaGlmdC1ldGNkLnN2Yy5jbHVzdGVyLmxvY2Fsgglsb2NhbGhvc3SCAzo6MYIM
         MTAuMC4xNDEuMjI4ggkxMjcuMC4wLjGCAzo6MYcQAAAAAAAAAAAAAAAAAAAAAYcE
         CgCN5IcEfwAAAYcQAAAFFFF098asdfNkS23123ANBgkqhkiG9w0BAQsFAAOCAQEA
         vlVzkjOIquj3uS0QxeixXWyCl8vApYanD/LqUN7ARSH5+vQ+Pw3qQJm1tdP3b7Yh
         6NZC1DuA/n/kwWOMXtfsbaobZjV5XlPKjp4Oz7SIFE4P8ZmyYxTNSordmbkS4ezS
         3QpJOF34IgyqxojTbQkWy4l3PZfpAt7ypWrbQiX8N9cSIKFicJN+uAzeDDM2qsh3
         Xxk2PQtmmxpfOe9VaAj1dnGg9YNiF7ECuOD8nZsctmx79rPeBOzI5N093op/eFNb
         yTE03WjyNP82xfiQnFfZQvqX6niasdf987ASfd4X1yh00ymxpK/cG1s7BPLSpaL1
         xCMBdF9nD3tq9v/oyKJjvQ==
         -----END CERTIFICATE-----
       key: |
         -----BEGIN RSA PRIVATE KEY-----
         FOOvpAIBAAKCAQEAruV1jbKMqi5XfCaz7Ey6Bn5XGYOJvYc4VWKpT4OZ/TzBarDO
         Okou0FU04klDK06Zajm13NKHDZjzhzcskO9xKQvFDanR17zqV0aX03MZR4pydbUv
         WLKSE9KAKA823298gnkeQ9sorUwLrtjnFHdUDwfagIOffmPiZPIFM1hABZ50B1FV
         xXCRdaAAIGKYvAg0ZcarE4LXSasdf/asdSk32kVMd2t7unA2nAIcxJmik4AcZxok
         oIgOIIIIIIL1LOToA0XYPxkuOcWTpIg6XCVQuI104SKZ9K7yZSWmEBm3E2uSWz++
         x/lYpw/vEjaWeaK9qdmCjXud+jceiCLjmiJF4QIDAQABAoIBADgvowo4eBQb+yL5
         VAfvxjtbzyN1LITkseZMYdQXlRrTr9dUoYv8VPm8xdaEbr207HhBvfkI8TYfEu03
         fmu5YIMtMsrm6XEDUc1j8laNvWtMQOUrpeA6zc7sJhvS5ys09gM6H+RwvaqeqYos
         SGA8zZZekYWDw3NZJ1wCnEUYbsjeEE298fjLfvs7vk5AswD2yyX2fF4wEBLwMi0L
         -----END RSA PRIVATE KEY-----
       url: https://localhost:2379/debug/pprof/profile?timeout=30
       
```

**Note**: As mentioned before, this measurement requires the `curl` command to be available in the target pods.
