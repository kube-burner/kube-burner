The Tests Workflow, defined in the `tests-k8s.yml` file, has two jobs: **test-k8s** and **ocp-wrapper**.

Tests are orchestrated using [BATS](https://bats-core.readthedocs.io/en/stable/)

#### Test K8s

For testing kube-burner on a real k8s environment, we use [Kind](https://kind.sigs.k8s.io/), with different K8s versions.

It creates a quick k8s deployment using containers with podman.

The **test-k8s** job performs the following steps:

1. Checks out the code.
1. Downloads the kube-burner binary artifact.
1. Installs Bats and the oc command-line tool.
1. Executes tests using Bats and generates a JUnit report.
1. Uploads the test results artifact.
1. Publishes the test report using the mikepenz/action-junit-report action.

#### Test OCP

For testing OCP, we use a real OCP

The **ocp-wrapper** job performs similar steps to the "test-k8s" job but with additional OpenShift-specific configurations and secrets.

#### Kube-burner tests executed

The kube-burner tests executed on each environment can be found on following files included in the folder `/test/`

- K8s: `test/test-k8s.bats`
- OCP: `test/test-ocp.bats`
