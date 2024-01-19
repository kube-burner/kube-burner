The testing Workflow, defined in the `tests-k8s.yml` file, runs tests defined in the `test` directory of the repository.

Tests are orchestrated with [bats](https://bats-core.readthedocs.io/en/stable/)

Tests can be executed locally with `make test`, some requirements are needed though:

- make
- bats
- kubectl
- podman or docker (required to run [kind](https://kind.sigs.k8s.io/))