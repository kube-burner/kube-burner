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

In addition, each indexer has its own configuration parameters.

----

The `elastic` indexer is configured by the parameters below:

| Option               | Description                                       | Type        | Example                                  | Default |
|----------------------|---------------------------------------------------|-------------|------------------------------------------|---------|
| esServers            | List of ES instances                              | List        | [https://elastic.apps.rsevilla.org:9200] | ""      |
| defaultIndex         | Default index to send the collected metrics into  | String      | kube-burner                              | ""      |
| insecureSkipVerify   | TLS certificate verification                      | Boolean     | true                                     | false   |
| summaryIndex         | Alternative index for benchmark summary           | String      | kube-burner-summary                      | ""      |
| extraMetadata        | File path holding arbitrary metadata to be indexed along with the benchmark summary | String | Example | metadata.yaml | "" |

**Note**: It's possible to index documents in an authenticated ES instance using the notation `http(s)://[username]:[password]@[address]:[port]` in the *esServers* parameter.
