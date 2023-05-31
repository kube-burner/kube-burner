The Pull Request Workflow, defined in the `pullrequest.yml` file, is triggered on push events to any branch. It has three jobs: **build**, **tests**, and **report_results**.

### Build

The "build" job uses the file `builders.yml` file to build binaries and images, performing the following steps:

1. Sets up Go 1.19.
1. Checks out the code.
1. Builds the code using the `make build` command.
1. Builds container images using the `make images` command
1. Installs the built artifacts using `sudo make install` command.
1. Uploads the built binary file as an artifact named **kube-burner**.

### Tests

{% include-markdown "tests.md" %}

### Report Results

The "report_results" performs the following steps:

1. Checks out the code.
1. Downloads the Kubernetes and OpenShift test results artifacts.
1. Publishes the test results and add a comment in the PR
