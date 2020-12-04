# Metrics profile

The metrics-profile flag points to a YAML or URL of a file containing a list of the prometheus queries kube-burner will collect for each job.
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


It's also possible to execute instant queries from kube-burner by adding the flag instant to the desired metric. These kind of queries are useful to get only one sample for a static metric such as the number of nodes or the kube-apiserver version.

```yaml
metrics:
  - query: kube_node_role
    metricName: nodeRoles
    instant: true
```

# Job Summary

In case indexing is enabled, at the end of each job, a document holding the job summary is indexed. This is useful to identify the parameters the job was executed with:

This document looks like:

```json
{
  "timestamp": "2020-11-13T13:55:31.654185032+01:00",
  "uuid": "bdb7584a-d2cd-4185-8bfa-1387cc31f99e",
  "metricName": "jobSummary",
  "elapsedTime": 8.768932955,
  "jobConfig": {
    "jobIterations": 10,
    "jobIterationDelay": 0,
    "jobPause": 0,
    "name": "kubelet-density",
    "objects": [
      {
        "objectTemplate": "templates/pod.yml",
        "replicas": 1,
        "inputVars": {
          "containerImage": "gcr.io/google_containers/pause-amd64:3.0"
        }
      }
    ],
    "jobType": "create",
    "qps": 5,
    "burst": 5,
    "namespace": "kubelet-density",
    "waitFor": null,
    "maxWaitTimeout": 43200000000000,
    "waitForDeletion": true,
    "podWait": false,
    "waitWhenFinished": true,
    "cleanup": true,
    "namespacedIterations": false,
    "verifyObjects": true,
    "errorOnVerify": false
  }
}
```
