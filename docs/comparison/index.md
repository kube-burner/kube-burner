# Comparison with other tools

Kube-burner is a Kubernetes performance and scale test orchestration toolset capable of creating, deleting, and patching Kubernetes resources at scale, with Prometheus metric collection and indexing. It was created by Red Hat and has been accepted as a CNCF Sandbox project. ClusterLoader2 (CL2), is the official Kubernetes scalability and performance testing framework, living within the kubernetes/perf-tests repository and maintained by SIG Scalability.

Kube-burner tests are imperative, job-based YAML files that define a set of operations to perform on a cluster, these operations can use a combination of parameters and templates able to create simulate complex workflows, on the other hand CL2 tests are also written in YAML using a semi-declarative paradigm. A test defines a set of jobs with a series of phases and measurements (e.g. Job 1, Create 10k pods and 2k cluster-ip services and wait for them to be ready, Job 2: path the previous pods) and specifies how quickly each state should be reached.

| Dimension | kube-burner | ClusterLoader2 |
|---|---|---|
| **Status** | CNCF Sandbox | Official k8s framework |
| **Primary target** | Vanilla Kubernetes; deep OpenShift support via (kube-burner-ocp)[https://github.com/kube-burner/kube-burner-ocp] | Vanilla Kubernetes |
| **Config style** | Imperative job-based YAML | Semi-declarative desired-state YAML |
| **Templating** | Go templates for resource YAML | Go templates + Module API for config reuse |
| **Operations** | Create, delete, read, patch and kubevirt operations | Create, update and delete  |
| **Rate control** | QPS and Burst | TuningSet-driven rate control |
| **Namespace handling** | Auto-creates namespaced iterations | Auto-managed namespaces with range control |
| **Workload churn** | Native churn job type | Not built-in; achievable via step sequencing |
| **Metrics source** | Arbitrary PromQL metrics from multiple Prometheus endpoints | Hardcoded queries in built-in measurements; arbitrary PromQL available via GenericPrometheusQuery |
| **Built-in measurements** | [Docs](https://github.com/kube-burner/kube-burner/blob/main/docs/measurements/index.md#measurements) | [Docs](https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/README.md#measurement) |
| **SLO validation** | Custom Prometheus alerting expressions + SLO checking built in measurement | Native SLO checking built into each measurement; test fails if violated |
| **Result storage** | Local JSON, Elasticsearch/OpenSearch and Prometheus TSDB | Local JSON |
| **Scale ceiling** | Proben to up to 10.000 nodes (AWS) | Proven to 65,000 nodes (GKE); k8s release-blocking at 5,000 nodes |
| **Command hook support** | Yes, can run arbitraty local commands during the different stages of the test | Limited (PodPeriodicCommand) |

Kube-burner is a tool designed to work in multiple environments and conditions, so the config flexibility has been a key feature since the beginning whereas CL2 is a tool designed to validate vanilla kuberntees SLOs where some platform specifics are assumed and configuration flexibility is more limited.