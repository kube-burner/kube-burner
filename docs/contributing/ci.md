# Continuous Integration Documentation

This documentation provides an overview of the different jobs and their execution in the CI workflow. The CI workflow consists of several jobs that automate various tasks such as building binaries and images, executing tests, generating and deploying documentation, creating releases, and uploading containers. Each job is triggered based on specific events.

## Pull Request workflow

{% include-markdown "ci/pullrequest.md" %}

## Release workflow

{% include-markdown "ci/release.md" %}
