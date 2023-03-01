# Indexers

`kube-burner` is able to **index the collected prometheus metrics** into a given Indexer. These metrics are indexed after the execution of the last Kube-burner's job.

## Indexer configuration

The indexer configuration is described in the `indexerConfig` section and can be configured with the following parameters:

| Option               | Description           | Type     | Example    | Default |
|----------------------|-----------------------|----------|------------|---------|
| enabled              | Enable indexing       | Boolean  | true       | false   |
| type                 | Type of indexer       | String   | elastic    | ""      |

### Elastic

Index documents in Elasticsearch 7 instances.

----

The `elastic` indexer can be configured by the parameters below:

| Option               | Description                                       | Type        | Example                                  | Default |
|----------------------|---------------------------------------------------|-------------|------------------------------------------|---------|
| esServers            | List of ES instances                              | List        | [https://elastic.apps.rsevilla.org:9200] | ""      |
| defaultIndex         | Default index to send the prometheus metrics into | String      | kube-burner                              | ""      |
| insecureSkipVerify   | TLS certificate verification                      | Boolean     | true                                     | false   |

**Note**: It's possible to index documents in an authenticated ES instance using the notation `http(s)://[username]:[password]@[address]:[port]` in the *esServers* parameter.

### Local

Writes collected metrics to local files.

----

The `local` indexer can be configured by the parameters below:

| Option               | Description                                       | Type        | Example                                  | Default |
|----------------------|---------------------------------------------------|-------------|------------------------------------------|---------|
| metricsDirectory     | Directory where collected metrics will be dumped into. It will be created if it doesn't exist previously | String      | /var/tmp/myMetrics  | collected-metrics       |
| createTarball        | Create metrics tarball                        | Boolean        | true | false      |
| tarballName          | Name of the metrics tarball                   | String         | myMetrics.tgz | kube-burner-metrics.tgz      |
