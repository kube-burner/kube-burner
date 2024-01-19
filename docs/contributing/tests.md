The Tests Workflow, defined in the `tests-k8s.yml` file

Tests are orchestrated using [BATS](https://bats-core.readthedocs.io/en/stable/), they can be executed locally with `make test`

#### Requirements

- make
- bats
- podman or docker (needed to spin-up a [kind](https://kind.sigs.k8s.io/) cluster)
