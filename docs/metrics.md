# Metrics

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

## Job Summary

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

## Metric exporting & importing

kube-burner provides the ability of generating a tarball containing the collected metrics. This feature is useful to store and import them later in a different environment. Metrics exporting is enabled setting to true the parameters `writeToFile` and `createTarball` from the configuration file. The tarball is generated after running running benchmark, but it can be also used in the `index` subcommand.

e.g.
```shell
$ kube-burner index --prometheus-url=https://prometheus-instance.domain.com --token=${token} --uuid=$(uuidgen) -c cfg.yml -m metrics.yaml
INFO[2021-06-23 12:07:07] Setting log level to info
INFO[2021-06-23 12:07:07] 👽 Initializing prometheus client
INFO[2021-06-23 12:07:07] Indexing metrics with UUID 0f618e71-c6bc-4b4e-9668-d59530c06e2f
INFO[2021-06-23 12:07:07] 🔍 Scraping prometheus metrics from 2021-06-23 11:07:07 +0200 CEST to 2021-06-23 12:07:07 +0200 CEST
INFO[2021-06-23 12:07:07] Range query: count(kube_namespace_created)
INFO[2021-06-23 12:07:07] Writing to: collected-metrics/namespaceCount.json
INFO[2021-06-23 12:07:07] Range query: sum(kube_pod_status_phase{}) by (phase)
INFO[2021-06-23 12:07:07] Writing to: collected-metrics/podStatusCount.json
INFO[2021-06-23 12:07:07] Range query: count(kube_secret_info{})
INFO[2021-06-23 12:07:07] Writing to: collected-metrics/secretCount.json
INFO[2021-06-23 12:07:07] Range query: count(kube_deployment_labels{})
INFO[2021-06-23 12:07:07] Writing to: collected-metrics/deploymentCount.json
INFO[2021-06-23 12:07:07] Range query: count(kube_configmap_info{})
INFO[2021-06-23 12:07:08] Writing to: collected-metrics/configmapCount.json
INFO[2021-06-23 12:07:08] Range query: count(kube_service_info{})
INFO[2021-06-23 12:07:08] Writing to: collected-metrics/serviceCount.json
INFO[2021-06-23 12:07:08] Instant query: kube_node_role
INFO[2021-06-23 12:07:08] Writing to: collected-metrics/nodeRoles.json
INFO[2021-06-23 12:07:08] Range query: sum(kube_node_status_condition{status="true"}) by (condition)
INFO[2021-06-23 12:07:08] Writing to: collected-metrics/nodeStatus.json
INFO[2021-06-23 12:07:08] Instant query: cluster_version{type="completed"}
INFO[2021-06-23 12:07:08] Writing to: collected-metrics/clusterVersion.json
INFO[2021-06-23 12:07:08] Metrics tarball generated at kube-burner-metrics-1624442828.tgz
```

Once we've enabled it, a tarball (`kube-burner-metrics-<timestamp>.tgz`) containing all metrics will be generated in the current working directory.
This tarball can be imported and indexed by kube-burner with the import subcommand. e.g.

```shell
$ kube-burner/bin/kube-burner import --config kubelet-config.yml --tarball kube-burner-metrics-1624441857.tgz
INFO[2021-06-23 11:39:40] Setting log level to info
INFO[2021-06-23 11:39:40] 📁 Creating indexer: elastic
INFO[2021-06-23 11:39:42] Importing tarball kube-burner-metrics-1624441857.tgz
INFO[2021-06-23 11:39:42] Importing metrics from doc.json
INFO[2021-06-23 11:39:43] Indexing [1] documents in kube-burner
INFO[2021-06-23 11:39:43] Successfully indexed [1] documents in 208ms in kube-burner
```
