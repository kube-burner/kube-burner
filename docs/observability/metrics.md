# Metric profile

The metric-collection feature is configured through a file pointed by the `metrics-profile` flag, which can point to a local path or URL of a YAML-formatted file containing a list of the Prometheus expressions. Kube-burner will perform those queries one by one, once all jobs are finished.

In a single job benchmark, the queries are executed using the benchmark start and end time as time range. In multiple job benchmarks, these queries are executed in a per job basis, and they take the different start and end times from the executed jobs.

The metrics profile file has the following structure:

```yaml
- query: irate(process_cpu_seconds_total{job=~".*(crio|etcd|controller-manager|apiserver|scheduler).*"}[2m])
  metricName: controlPlaneCPU

- query: sum(irate(node_cpu_seconds_total[2m])) by (mode,instance)
  metricName: nodeCPU
```

The `query` field holds the Prometheus expression to evaluate, and `metricName` controls the value that kube-burner will set on the `metricName` field of the generated documents. This is useful to identify metrics from a specific query. More information is available in the [metric format section](#metric-format).

## Instant queries

In addition to the default range queries, kube-burner has the ability execute [instant queries](https://prometheus.io/docs/prometheus/latest/querying/api/#instant-queries) against the provided Prometheus API. This can be enaled by enabling the field `instant` to the desired metric.

```yaml
- query: kube_node_role
  metricName: nodeRoles
  instant: true
```

!!! info
    When using instant queries, at least two documents are generated, one resulting from scraping the last timestamp of the job, which would have the configued `metricName` field and an another one resulting from scraping the first timestamp of the job, the `metricName` of document is appended the `-start` suffix.

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

Notice that kube-burner enriches the query results by adding some extra fields like `uuid`, `query`, `metricName` and `jobConfig`.
!!! info
    These extra fields are especially useful at the time of identifying and representing the collected metrics.

## Using the elapsed variable

There is a special go-template variable that can be used within the Prometheus expressions of a metric profile; the variable `elapsed` is automatically populated with the job duration, in seconds. This variable is especially useful in PromQL expressions using [aggregations over time functions](https://prometheus.io/docs/prometheus/latest/querying/functions/#aggregation_over_time).

For example, the following expression gets the top 3 datapoints with the average CPU usage kubelets processes in the cluster.

```yaml
- query: irate(process_cpu_seconds_total{service="kubelet",job="kubelet"}[2m]) * 100 and on (node) topk(3,avg_over_time(irate(process_cpu_seconds_total{service="kubelet",job="kubelet"}[2m])[{{ .elapsed }}:]))
  metricName: top3KubeletCPU
  instant: true
```

!!! info
    Note that in the [time-range:] notation, the colon specifies to get the values for the given duration.

Examples of metrics profiles can be found in the [examples directory](https://github.com/kube-burner/kube-burner/tree/master/examples/). There are also Elasticsearch based Grafana dashboards available in the same examples directory.
