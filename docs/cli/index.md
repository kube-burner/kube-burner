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
  destroy      Destroy benchmark assets
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

- `uuid`: Benchmark ID. This is essentially an arbitrary string that is used for different purposes along the benchmark. For example, to label the objects created by kube-burner as mentioned in the [reference chapter](../reference/configuration.md#default-labels). By default, it is auto-generated.
- `config`: Path or URL to a valid configuration file. See details about the configuration schema in the [reference chapter](../reference/configuration.md).
- `configmap`: In case of not providing the `--config` flag, kube-burner is able to fetch its configuration from a given `configMap`. This variable configures its name. kube-burner expects the configMap to hold all the required configuration: config.yml, metrics.yml, and alerts.yml. Where metrics.yml and alerts.yml are optional.
- `namespace`: Name of the namespace where the configmap is.
- `log-level`: Logging level, one of: `debug`, `error`, `info` or `fatal`. Default `info`.
- `metrics-endpoint`: Path to a valid metrics endpoint file.
- `skip-tls-verify`: Skip TLS verification for Prometheus. The default is `true`.
- `timeout`: Kube-burner benchmark global timeout. When timing out, return code is 2. The default is `4h`.
- `kubeconfig`: Path to the kubeconfig file.
- `kube-context`: The name of the kubeconfig context to use.
- `user-metadata`: YAML file path containing custom user-metadata to be indexed along with the `jobSummary` document.
- `user-data`: YAML or JSON file path containing input variables for rendering the configuration file.
- `allow-missing`: Allow missing keys in the config file. Needed when using the [`default`](https://masterminds.github.io/sprig/defaults.html) template function
- `set`: Set arbitrary `key=value` pairs to override values in the configuration file. Similar to Helm's `--set` flag, this allows you to override base YAML values directly from the command line. Multiple values can be specified by separating them with commas or by using multiple `--set` flags. Nested keys are supported using dot notation, and array indices can be used with numeric keys (e.g., `jobs.0.name=test`).

!!! Note "Prometheus authentication"
    Both basic and token authentication methods need permissions able to query the given Prometheus endpoint.

With the above, running a kube-burner benchmark would be as simple as:

```console
kube-burner init -c cfg.yml --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

To override configuration values directly from the command line using the `--set` flag:

```console
kube-burner init -c ./examples/workloads/kubelet-density/kubelet-density.yml \
  --set global.gc=false,global.timeout=1h \
  --set jobs.0.name=test,jobs.0.jobIterations=5
```

Kube-burner also supports remote configuration files served by a web server. To use it, rather than a path, pass a URL. For example:

```console
kube-burner init -c http://web.domain.com:8080/cfg.yml --uuid 67f9ec6d-6a9e-46b6-a3bb-065cde988790`
```

To scrape metrics from multiple endpoints, the  `init` command can be triggered. For example:

```console
kube-burner init -c cluster-density.yml -e metrics-endpoints.yaml
```

A metrics-endpoints.yaml file with valid keys for the `init` command would look like the following:

```yaml
- endpoint: http://localhost:9090
  token: <token>
  metrics: [metrics.yaml]
  indexer:
    type: local
- endpoint: http://remotehost:9090
  username: foo
  password: bar
  alerts: [alert-profile.yaml]
```

### Exit codes

Kube-burner has defined a series of exit codes that can help to programmatically identify a benchmark execution error.

| Exit code | Meaning |
|--------|--------|
| 0 | Benchmark execution finished normally |
| 1 | Generic exit code, returned on a unrecoverable error (i.e: API Authorization error or config parsing error) |
| 2 | Benchmark timeout, returned when kube-burner's execution time exceeds the value passed in the `--timeout` flag |
| 3 | Alerting error, returned when a `error` or `critical` level alert is fired |
| 4 | Measurement error, returned on some measurements error conditions, like `thresholds` |

## Index

This subcommand can be used to collect and index the metrics from a given time range. The time range is given by:

- `start`: Epoch start time. Defaults to one hour before the current time.
- `end`: Epoch end time. Defaults to the current time.

## Measure

This subcommand can be used to collect measurements for resources in the cluster by observing them in real-time using Kubernetes informers and watchers. Unlike the `init` command, this subcommand does not run any workload, only observes existing resources and collects measurements as they change.

The measure command uses Kubernetes informers to watch the cluster and collect measurements for resources that match the specified criteria. It supports all measurement types available in kube-burner (podLatency, serviceLatency, jobLatency, pprof, etc.) as configured in the measurements section of the configuration file.

It requires a configuration file with the measurements to collect. The configuration file should include the `measurements` section defining which measurements to collect. For example:

```yaml
metricsEndpoints:
  - indexer:
      metricsDirectory: /tmp/kube-burner
      type: local
global:
  measurements:
   - name: podLatency
   - name: pprof
     pprofInterval: 60s
     pprofDirectory: pprof-data
     nodeAffinity:
       node-role.kubernetes.io/worker: ""
     pprofTargets:
       - name: kubelet-heap
         url: https://localhost:10250/debug/pprof/heap
       - name: crio-heap
         url: http://localhost/debug/pprof/heap 
         unixSocketPath: /var/run/crio/crio.sock
```

Example usage:

```console
kube-burner measure -c kube-burner-measurements.yml --duration=30m --selector=app=myapp
```

!!! Note "Measurement types"
    Find extended information about the different measurement types available in kube-burner [here](../measurements/index.md)

## Check alerts

This subcommand can be used to evaluate alerts configured in the given alert profile. Similar to `index`, the time range is given by the `start` and `end` flags.

## Destroy

This subcommand uses the provided configuration file to destroy the objects declared in it, using the defined deletion strategy. Can be used as an alternative approach to perform the benchmark garbage collection.

!!! Note
    The same config rendering logic with environment variables or user-data file applies here. It's up to the user to set the them accordingly to ensure the deletion of the desired objects.

## Health Check

The `health-check` subcommand assesses the status of nodes within the cluster. It provides information on the overall health of the cluster, indicating whether it is in a healthy state. In the event of an unhealthy cluster, the subcommand returns a list of nodes that are not in a "Ready" state, helping users identify and address specific issues affecting cluster stability.

## Completion

Generates bash a completion script that can be imported with:
`. <(kube-burner completion)`

Or permanently imported with:
`kube-burner completion > /etc/bash_completion.d/kube-burner`

!!! note
    the `bash-completion` utils must be installed for the kube-burner completion script to work.
