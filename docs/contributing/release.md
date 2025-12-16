The Release workflow, defined in the `release.yml` file, when a new tag is pushed it triggers: the workflows defined `ci-tests.yml`, `gorelease.yml`, `image-upload.yml` and `docs.yml` are executed following the order below:

```mermaid
graph LR
graph LR
  A[new tag pushed] --> B[ci-tests];
  B --> C[gorelease];
  B --> E[docs];
  B --> D[image-upload];
```

#### Release Build

This job uses the `Create a new release of project` workflow defined in `gorelease.yml` to create a new release of the project performing the following steps:

1. Checks out the code into the Go module directory.
2. Sets up Go 1.19.
3. Runs GoReleaser to create a new release, including the removal of previous distribution files.

#### Image Upload

This job uses the `Upload Containers to Quay` workflow defined in `image-upload.yml` to upload containers to the Quay registry for multiple architectures (arm64, amd64, ppc64le, s390x) performing the following steps:

1. Installs the dependencies required for multi-architecture builds.
2. Checks out the code.
3. Sets up Go 1.19.
4. Logs in to Quay using the provided QUAY_USER and QUAY_TOKEN secrets.
5. Builds the kube-burner binary for the specified architecture.
6. Builds the container image using the make images command, with environment variables for architecture and organization.
7. Pushes the container image to Quay using the make push command.

The "manifest" job builds a container manifest and runs after the "containers" job. It performs the following steps:

1. Checks out the code.
1. Logs in to Quay using the provided QUAY_USER and QUAY_TOKEN secrets.
1. Creates and pushes the container manifest using the make manifest command, with the organization specified.

#### Docs Update

Uses the `Deploy docs` workflow defined in `docs.yml` to generate and deploy the documentation performing the following steps:

1. Checks out the code.
2. Sets up Python 3.x.
3. Exports the release tag version as an environment variable.
4. Sets up the Git configuration for documentation deployment.
5. Installs the required dependencies, including mkdocs-material and mike.
6. Deploys the documentation using the mike deploy command, with specific parameters for updating aliases and including the release tag version in the deployment message.
