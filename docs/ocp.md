# OpenShift Wrapper

The kube-burner binary brings a very opinionated OpenShift wrapper designed to simplify the execution of different workloads in this kubernetes distribution.
This wrapper is hosted under the `kube-burner ocp` subcommand that currently looks like:

```console
$ ./bin/amd64/kube-burner ocp
This subcommand is meant to be used against OpenShift clusters and serve as a shortcut to trigger well-known workloads

Usage:
  kube-burner ocp [command]

Available Commands:
  cluster-density    Runs cluster-density workload
  node-density       Runs node-density workload
  node-density-heavy Runs node-density-heavy workload

Flags:
      --burst int          Burst (default 20)
      --es-index string    Elastic Search index
      --es-server string   Elastic Search endpoint
  -h, --help               help for ocp
      --qps int            QPS (default 20)
      --uuid string        Benchmark UUID (default "ff60bd1c-df27-4713-be3e-6b92acdd4d72")

Global Flags:
      --loglevel string   Allowed values: trace, debug, info, warn, error, fatal (default "info")

Use "kube-burner ocp [command] --help" for more information about a command.

```

## Usage

In order to trigger one of the supported workloads using this subcommand you have to run kube-burner within the directory of the desired workload. The workloads are stored in the ocp-config directory of this repository. i.e:

Running node-density with 100 pods per node

```console
~/kube-burner $ cd ocp-config/node-density
~/kube-burner/ocp-config/node-density $ kube-burner ocp node-density --pods-per-node=100
```

With the command above, the wrapper will calculate the required number of pods to deploy across all worker nodes of the cluster.

This wrapper provides the following benefits:

- Provides a simplified execution of the supported workloads
- Indexes OpenShift metadata along with the Benchmark result, this document can be found with the following query: `uuid: <benchmkark-uuid> AND metricName.keyword: "clusterMetadata"`
- Prevents modifying configuration files to tweak some of the parameters of the workloads
- Discovers the Prometheus URL and authentication token, so the user does not have to perform those operations before using them.
