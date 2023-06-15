# Metric profile

The metric-collection feature is configured through a file pointed by the `metrics-profile` flag. Which can point to a local path or URL of a YAML formatted file containing a list of the prometheus expressions. Kube-burner will perform those queries one by one once all benchmark's jobs are done and transform them into a JSON formatted list.

In a single job benchmark, the queries are executed using the benchmark start and end time as time range. In multiple job benchmarks these queries are executed in a per job basis, and they take the different start, end time from the executed jobs.

The metrics profile file has the following structure:

```yaml
- query: irate(process_cpu_seconds_total{job=~".*(crio|etcd|controller-manager|apiserver|scheduler).*"}[2m])
  metricName: controlPlaneCPU

- query: sum(irate(node_cpu_seconds_total[2m])) by (mode,instance)
  metricName: nodeCPU
```

Where `query` holds the prometheus expression to evaluate, and `metricName` controls the value that kube-burner will set on the `metricName` field, useful to identify metrics from a specific query. More info in the [metric format section](#metric-format)

In addition to range queries, kube-burner has the ability perform instant queries by adding the field `instant` to the desired metric. This kind of query is especially useful to get only one sample of a *static* metric such as a component version or the number of nodes of the cluster.

```yaml
- query: kube_node_role
  metricName: nodeRoles
  instant: true
```

## Metric format

The collected metrics have the following shape:

```json
[
  {
    "timestamp": "2021-06-23T11:50:15+02:00",
    "labels": {
      "instance": "ip-10-0-219-170.eu-west-3.compute.internal",
      "mode": "user"
    },
    "value": 0.3300880234732172,
    "uuid": "<UUID>",
    "query": "sum(irate(node_cpu_seconds_total[2m])) by (mode,instance) > 0",
    "metricName": "nodeCPU",
    "jobConfig": {
      "truncated_job_configuration": "foobar"
    }
  },
  {
    "timestamp": "2021-06-23T11:50:45+02:00",
    "labels": {
      "instance": "ip-10-0-219-170.eu-west-3.compute.internal",
      "mode": "user"
    },
    "value": 0.31978102677038506,
    "uuid": "<UUID>",
    "query": "sum(irate(node_cpu_seconds_total[2m])) by (mode,instance) > 0",
    "metricName": "nodeCPU",
    "jobConfig": {
      "truncated_job_configuration": "foobar"
    }
  }
]
```

Notice that kube-burner enriches the query results by adding some extra fields like `uuid`, `query`, `metricName` and `jobName`.
!!! info
    These extra fields are specially useful at the time of identifying and representing the collected metrics.

## Aggregating timeseries into a single document

Kube-burner provides a mechanism to aggregate a Prometheus timeseries into a single value.
The current supported aggregations are:

- `avg`: Average value of the timeseries
- `max`: Maximum value of the timeseries
- `min`: Minimum value of the timeseries
- `stdev`: Standard deviation of the timeseries values
- `99`: 99th percentile of the timeseries values
- `95`: 95th percentile of the timeseries values
- `90`: 90th percentile of the timeseries values
- `50`: 50th percentile of the timeseries values

This feature is configured throught the metrics-profile configuration file as follows:

```yaml
# Average and maximum CPU usage observed in all worker's kubelets processes
- query: irate(process_cpu_seconds_total{service="kubelet",job="kubelet"}[2m]) and on (node) kube_node_role{role="worker"}
  metricName: cpu-kubelet
  aggregations: [avg, max]
```

The above configuration will result in two documents:

```json
[
  {
    "uuid": "<UUID>",
    "timestamp": "2023-06-12T23:18:49.285718843Z",
    "value": 263544832,
    "query": "irate(process_cpu_seconds_total{service=\"kubelet\",job=\"kubelet\"}[2m]) and on (node) kube_node_role{role=\"worker\"}",
    "metricName": "cpu-kubelet",
    "jobConfig": {
      "truncated_job_configuration": "foobar"
    },
    "aggregation": "avg"
  },
  {
    "uuid": "<UUID>",
    "timestamp": "2023-06-12T23:18:49.285718843Z",
    "value": 363543813,
    "query": "irate(process_cpu_seconds_total{service=\"kubelet\",job=\"kubelet\"}[2m]) and on (node) kube_node_role{role=\"worker\"}",
    "metricName": "cpu-kubelet",
    "jobConfig": {
      "truncated_job_configuration": "foobar"
    },
    "aggregation": "max"
  }
]
```

## Using the elapsed variable

There's a special go-template variable that can be used within the prometheus expressions of a metric profile, the variable `elapsed` is automatically populeted with the job duration duration, in minutes. This variable is specially useful in PromQL expressions using [aggregations over time functions](https://prometheus.io/docs/prometheus/latest/querying/functions/#aggregation_over_time).

For example, the following expression gets the top 3 CPU usage kubelets processes across the cluster.

```yaml
- query: irate(process_cpu_seconds_total{service="kubelet",job="kubelet"}[2m]) * 100 and on (node) topk(3,avg_over_time(irate(process_cpu_seconds_total{service="kubelet",job="kubelet"}[2m])[{{ .elapsed }}:]))
  metricName: top3KubeletCPU
```

Examples of metrics profiles can be found in the [examples directory](https://github.com/cloud-bulldozer/kube-burner/tree/master/examples/). There're are also ElasticSearch based grafana dashboards available in the same examples directory.
