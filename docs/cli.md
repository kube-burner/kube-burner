# CLI

kube-burner is a tool written in Golang that can be used to stress Kubernetes clusters by creating, deleting, and patching resources at a
given rate. The actions taken by this tool are highly customizable and their available subcommands are detailed below:

```console
$ kube-burner help
Kube-burner 🔥

Tool aimed at stressing a kubernetes cluster by creating or deleting lots of objects.

Usage:
  kube-burner [command]

Available Commands:
  check-alerts Evaluate alerts for the given time range
  completion   Generates completion scripts for bash shell
  destroy      Destroy old namespaces labeled with the given UUID.
  health-check Check for Health Status of the cluster
  help         Help about any command
  import       Import metrics tarball
  index        Index kube-burner metrics
  init         Launch benchmark
  measure      Take measurements for a given set of resources without running workload
  version      Print the version number of kube-burner

Flags:
  -h, --help               help for kube-burner
      --log-level string   Allowed values: debug, info, warn, error, fatal (default "info")

Use "kube-burner [command] --help" for more information about a command.
```

## Init

This is the main subcommand; it triggers a new kube-burner benchmark and it supports the these flags:

- `uuid`: Benchmark ID. This is essentially an arbitrary string that is used for different purposes along the benchmark. For example, label the objects created by kube-burner as mentioned in the [reference chapter](/kube-burner/configuration/#default-labels). By default, it is auto-generated.
- `config`: Path or URL to a valid configuration file. See details about the configuration schema in the [reference chapter](/kube-burner/configuration/).
- `configmap`: In case of not providing the `--config` flag, kube-burner is able to fetch its configuration from a given `configMap`. This variable configures its name. kube-burner expects the configMap to hold all the required configuration: config.yml, metrics.yml, and alerts.yml. Where metrics.yml and alerts.yml are optional.
- `namespace`: Name of the namespace where the configmap is.
- `log-level`: Logging level, one of: `debug`, `error`, `info` or `fatal`. Default `info`.
- `prometheus-url`: Prometheus endpoint, required for metrics collection. For example: `https://prometheus-k8s-openshift-monitoring.apps.rsevilla.stress.mycluster.example.com`
- `metrics-profile`: Path to a valid metrics profile file. The default is `metrics.yml`.
- `metrics-endpoint`: Path to a valid metrics endpoint file.
- `token`: Prometheus Bearer token.
- `username`: Prometheus username for basic authentication.
- `password`: Prometheus password for basic authentication.
- `skip-tls-verify`: Skip TLS verification for Prometheus. The default is `true`.
- `step`: Prometheus step size. The default is `30s`.
- `timeout`: Kube-burner benchmark global timeout. When timing out, return code is 2. The default is `4h`. 
- `kubeconfig`: Path to the kubeconfig file.
- `kube-context`: The name of the kubeconfig context to use.
- `user-metadata`: YAML file path containing custom user-metadata to be indexed.

!!! Note "Prometheus authentication"
    Both basic and token authentication methods need permissions able to query the given Prometheus endpoint.

With the above, running a kube-burner benchmark would be as simple as:

```console
kube-burner init -c cfg.yml -u https://prometheus-k8s-openshift-monitoring.apps.rsevilla.stress.mycluster.example.com -t ${token} --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

Kube-burner also supports remote configuration files served by a web server. To use it, rather than a path, pass a URL. For example:

```console
kube-burner init -c http://web.domain.com:8080/cfg.yml -t ${token} --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

To scrape metrics from multiple endpoints, the  `init` command can be triggered. For example:

```console
kube-burner init -c cluster-density.yml -e metrics-endpoints.yaml
```

A metrics-endpoints.yaml file with valid keys for the `init` command would look like the following:

```yaml
- endpoint: http://localhost:9090
  token: <token>
  profile: metrics.yaml
  alertProfile: alert-profile.yaml
- endpoint: http://remotehost:9090
  username: foo
  password: bar
```

!!! Note
    Options `profile` and `alertProfile` are optional. If not provided, the options will be taken from the CLI flags first. Otherwise, they are populated with the default values. Invalid keys are ignored.

## Index

This subcommand can be used to collect and index the metrics from a given time range. The time range is given by:

- `start`: Epoch start time. Defaults to one hour before the current time.
- `end`: Epoch end time. Defaults to the current time.

## Measure

This subcommand can be used to collect measurements for a given set of resources which were part of a workload ran in past and are still present on the cluster (i.e only supports podLatency as of today).
We can specify a list of namespaces and selector labels as input.

- `namespaces`: comma-separated list of namespaces provided as a string input. This is optional, by default all namespaces are considered.
- `selector`: comma-separated list of selector labels in the format key1=value1,key2=value2. This is optional, by default no labels will be used for filtering.

!!! Note
    This subcommand should only be used to fetch measurements of a workload ran in the past. Also those resources should be active on the cluster. For present cases, please refer to the alternate options in this tool.

## Check alerts

This subcommand can be used to evaluate alerts configured in the given alert profile. Similar to `index`, the time range is given by the `start` and `end` flags.

## Destroy

This subcommand requires the `uuid` flag to destroy all namespaces labeled with `kube-burner-uuid=<UUID>`.

## Health Check

The `health-check` subcommand assesses the status of nodes within the cluster. It provides information on the overall health of the cluster, indicating whether it is in a healthy state. In the event of an unhealthy cluster, the subcommand returns a list of nodes that are not in a "Ready" state, helping users identify and address specific issues affecting cluster stability.

## Completion

Generates bash a completion script that can be imported with:
`. <(kube-burner completion)`

Or permanently imported with:
`kube-burner completion > /etc/bash_completion.d/kube-burner`

!!! note
    the `bash-completion` utils must be installed for the kube-burner completion script to work.
