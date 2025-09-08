The testing Workflow, defined in the `tests-k8s.yml` file, runs tests defined in the `test` directory of the repository.

Tests are orchestrated with [bats](https://bats-core.readthedocs.io/en/stable/)

Tests can be executed locally with `make test`, some requirements are needed though:

- make
- bats
- kubectl
- podman or docker (required to run [kind](https://kind.sigs.k8s.io/))

### Running test with Podman

Since the test suite includes KubeVirt VMs, it must run with rootful Podman.
Either run the tests as root, or access the rootful Podman socket.

#### Allow access to the rootful Podman socket

Assuming the user is in `wheel` group please do the following (one time):

As root, create a Drop-In file `/etc/systemd/system/podman.socket.d/10-socketgroup.conf`
with the following content:
```
[Socket]
SocketGroup=wheel
ExecStartPost=/usr/bin/chmod 755 /run/podman
```

The 1st line is needed in order to create the socket accessible by the `wheel` group.
2nd line because systemd-tmpfiles recreates the folder as root:root without group reading rights.

Stop `podman.socket` if it is running,
reload the daemon `systemctl daemon-reload` since we changed the systemd settings
and restart it again `systemctl enable --now podman.socket`

#### Running the test

Instruct Podman to communicate with the rootful Podman socket by setting the environment variable:
```
CONTAINER_HOST=unix://run/podman/podman.sock make test-k8s
```

### Running test with an External Cluster

By default, the tests start a local cluster using [kind](https://kind.sigs.k8s.io/).
Instead, you can use an already existsing cluster.

#### Prerequisites

Since the test suite includes KubeVirt VMs, the cluster must include the `kubevirt` operator.
See the [Installation](https://kubevirt.io/user-guide/cluster_admin/installation/) guide for details.

#### Kubeconfig

Either save the kubeconfig file under `~/.kube/config` or set the environment variable `KUBECONFIG` to its location

#### Run the tests

In order to instruct the tests to use the existing cluster set `USE_EXISTING_CLUSTER=yes` when calling `make`.

### Execute a subset of the tests

The list of executed tests may be filtered by setting the environment variable `TEST_FILTER`:

```bash
TEST_FILTER="datavolume" make test-k8s
```

### Running local Kind to use as an External Cluster

Starting Kind seperately from the test will reduce test setup and teardown time during development

#### Prerequisites
Follow the instuctions in [Running test with Podman](#running-test-with-podman)

#### Create Kind

Run:
```
hack/start_kind.sh
```

#### Customize

Customize Kind and Kubernetes versions by setting `KIND_VERSION` and/or `K8S_VERSION`

#### Run the tests
Follow the instuctions in [Running test with an External Cluster](#running-test-with-an-external-cluster)
using the `kubeconfig` file created by the Kind installation