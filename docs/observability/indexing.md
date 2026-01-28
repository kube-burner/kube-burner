# Indexing

Kube-burner can collect metrics and alerts from prometheus and from measurements and send them to the configured indexers.

## Metrics endpoints

The logic to configure metric collection and indexing is established by the `metricsEndpoints` field of the configuration file, this field is a list of objects with the following structure:

| Field     | Description     | Example   |
| --------- | --------------- | --------- |
| `endpoint` | Define the prometheus endpoint to scrape | `https://prom.my-domain.com` |
| `username` | Prometheus username (Basic auth) | `username` |
| `password` | Prometheus password (Basic auth) | `topSecret` |
| `token` | Prometheus bearer token (Bearer auth) | `yourTokenDefinition` |
| `step` | Prometheus step size, used when scraping it, by default `30s` | `1m` |
| `skipTLSVerify` | Skip TLS certificate verification, `true` by default | `true` |
| `metrics` | List of metrics files | `[metrics.yml, more-metrics.yml]` |
| `alerts` | List of alerts files | `[alerts.yml, more-alerts.yml]` |
| `indexer` | Indexer configuration | [indexers](#indexers) |
| `alias`   | Indexer alias, an arbitrary string required to send measurement results to an specific indexer  | `my-indexer` |

!!! Note
    Info about how to configure [metrics-profiles](metrics.md) and [alerts-profiles](alerting.md)

## Indexers

Configured by the `indexer` field, it defines an indexer for the Prometheus endpoint, making all collected metrics to be indexed in it.
Depending on the indexer, different configuration parameters need to be specified. The type of indexer is configured by the field `type`

| Option    | Description     | Supported values   |
| --------- | --------------- | ------- |
| `type`    | Type of indexer | `elastic`, `opensearch`, `local`|

## Example

An example of how the `metricsEndpoints` section would look like in the configuration is:

```yaml
metricsEndpoints:
  - endpoint: http://localhost:9090
    alias: os-indexing
    alerts:
    - alert-profile.yaml
  - endpoint: https://remote-endpoint:9090
    metrics:
    - metrics-profile.yaml
    indexer:
      type: local
      metricsDirectory: my-metrics
```

In the example above, two endpoints are defined, the first will be used to scrape for the alerts defined in the file `alert-profile.yaml` and the second one will be used to scrape the metrics defined in `metrics-profile.yaml`, these metrics will be indexed by the configured indexer.

!!! info
    Configuring an indexer in an endpoint is only required when any metrics profile is configured

### Elastic/OpenSearch

Send collected documents to Elasticsearch7 or OpenSearch instances.

The `elastic` or `opensearch` indexer can be configured by the parameters below:

| Option               | Description                                       | Type    | Default |
| -------------------- | ------------------------------------------------- | ------- | ------- |
| `esServers`          | List of Elasticsearch or OpenSearch URLs          | List    | []      |
| `defaultIndex`       | Default index to send the Prometheus metrics into | String  | ""      |
| `insecureSkipVerify` | TLS certificate verification                      | Boolean | false   |

OpenSearch is backwards compatible with Elasticsearch and kube-burner does not use any version checks. Therefore, kube-burner with OpenSearch indexing should work as expected.

!!! info
    It is possible to index documents in an authenticated Elasticsearch or OpenSearch instance using the notation `http(s)://[username]:[password]@[address]:[port]` in the `esServers` parameter.

### Local

This indexer writes collected metrics to local files.

The `local` indexer can be configured by the parameters below:

| Option             | Description                           | Type    | Default                 |
| ------------------ | ------------------------------------- | ------- | ----------------------- |
| `metricsDirectory` | Collected metric will be dumped here. | String  | collected-metrics       |
| `createTarball`    | Create metrics tarball                | Boolean | false                   |
| `tarballName`      | Name of the metrics tarball           | String  | kube-burner-metrics.tgz |

## Job Summary

When an indexer is configured, a job summary document is indexed at the end of the job. It contains timestamps of the execution phase, performance metrics, and execution status.

The job summary document includes the following fields:

| Field                  | Description                                                                                          | Type             | Always Present |
| ---------------------- | ---------------------------------------------------------------------------------------------------- | ---------------- | -------------- |
| `timestamp`            | Start timestamp of the job execution                                                                 | String (ISO 8601) | Yes            |
| `endTimestamp`         | End timestamp of the job execution                                                                   | String (ISO 8601) | Yes            |
| `churnStartTimestamp`  | Start timestamp of the churn phase (only present if churn is enabled)                               | String (ISO 8601) | No             |
| `churnEndTimestamp`    | End timestamp of the churn phase (only present if churn is enabled)                                  | String (ISO 8601) | No             |
| `elapsedTime`          | Total execution time in seconds                                                                      | Float            | Yes            |
| `achievedQps`          | Achieved queries per second (calculated as object operations / elapsed time)                       | Float            | No             |
| `uuid`                 | Unique identifier for this benchmark run                                                              | String           | Yes            |
| `metricName`           | Always set to `"jobSummary"` for identification                                                     | String           | Yes            |
| `version`               | kube-burner version and git commit in format `version@gitCommit`                                      | String           | No             |
| `passed`                | Whether the job execution passed (true) or failed (false)                                            | Boolean          | Yes            |
| `executionErrors`       | Error messages from job execution, if any                                                            | String           | No             |
| `jobConfig`             | Complete job configuration object (see [configuration reference](../reference/configuration.md))     | Object           | Yes            |

Additionally, any metadata provided via the `--user-metadata` flag or through the metrics scraper's summary metadata is merged into the job summary document.

Example job summary document:

```json
{
  "timestamp": "2023-08-29T00:17:27.942960538Z",
  "endTimestamp": "2023-08-29T00:18:15.817272025Z",
  "churnStartTimestamp": "2023-08-29T00:17:45.000000000Z",
  "churnEndTimestamp": "2023-08-29T00:18:00.000000000Z",
  "elapsedTime": 48.0,
  "achievedQps": 0.333,
  "uuid": "83bfcb20-54f1-43f4-b2ad-ad04c2f4fd16",
  "metricName": "jobSummary",
  "version": "v1.10.0@4c9c3f43db83",
  "passed": true,
  "executionErrors": "",
  "jobConfig": {
    "jobIterations": 1,
    "name": "cluster-density-v2",
    "jobType": "create",
    "qps": 20,
    "burst": 20,
    "namespace": "cluster-density-v2",
    "maxWaitTimeout": 14400000000000,
    "waitForDeletion": true,
    "waitWhenFinished": true,
    "cleanup": true,
    "namespacedIterations": true,
    "iterationsPerNamespace": 1,
    "verifyObjects": true,
    "errorOnVerify": true,
    "preLoadImages": true,
    "preLoadPeriod": 15000000000,
    "churnPercent": 10,
    "churnDuration": 3600000000000,
    "churnDelay": 120000000000
  }
}
```

!!! Note
    Fields marked with `omitempty` in the JSON structure (such as `churnStartTimestamp`, `churnEndTimestamp`, `achievedQps`, `version`, and `executionErrors`) will not be present in the indexed document when they have no value or are not applicable.

## Metric exporting & importing

When using the `local` indexer, it is possible to dump all of the collected metrics into a tarball, which you can import later. This is useful in disconnected environments, where kube-burner does not have direct access to an Elasticsearch instance. Metrics exporting can be configured by `createTarball` field of the indexer config as noted in the [local indexer](#local).

The metric exporting feature is available through the `init` and `index` subcommands. Once you enabled it, a tarball (`kube-burner-metrics-<timestamp>.tgz`) containing all metrics is generated in the current working directory. This tarball can be imported and indexed by kube-burner with the `import` subcommand. For example:

```console
$ kube-burner/bin/kube-burner import --config kubelet-config.yml --tarball kube-burner-metrics-1624441857.tgz
INFO[2021-06-23 11:39:40] 📁 Creating indexer: elastic
INFO[2021-06-23 11:39:42] Importing tarball kube-burner-metrics-1624441857.tgz
INFO[2021-06-23 11:39:42] Importing metrics from doc.json
INFO[2021-06-23 11:39:43] Indexing [1] documents in kube-burner
INFO[2021-06-23 11:39:43] Successfully indexed [1] documents in 208ms in kube-burner
```

## Scraping from multiple endpoints

It is possible to scrape from multiple Prometheus endpoints and send the results to the target indexer with the `init` and `index` subcommands. This feature is configured by the flag `--metrics-endpoint`, which points to a YAML file with the required configuration.

A valid file provided to the `--metrics-endpoint` looks like this:

```yaml
- endpoint: http://localhost:9090 # This is one of the Prometheus endpoints
  token: <token> # Authentication token
  metrics: [metrics.yaml] # Metrics profiles to use for this endpoint
  indexer:
    - type: local
- endpoint: http://remotehost:9090 # Another Prometheus endpoint
  token: <token>
  alerts: [alerts.yaml] # Alert profile, when metrics is not defined, defining an indexer is optional
```

!!! Note
    The configuration provided by the `--metrics-endpoint` flag has precedence over the parameters specified in the config file.
