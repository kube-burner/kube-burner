<img src='./media/horizontal/kube-burner-horizontal-color.png' width='65%'>

[![Go Report Card](https://goreportcard.com/badge/github.com/kube-burner/kube-burner)](https://goreportcard.com/report/github.com/kube-burner/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# What is Kube-burner

Kube-burner is a Kubernetes performance and scale test orchestration toolset. It provides multi-faceted functionality, the most important of which are summarized below.

- Create, delete and patch Kubernetes resources at scale.
- Prometheus metric collection and indexing.
- Measurements.
- Alerting.

Kube-burner is a binary application written in golang that makes extensive usage of the official k8s client library, [client-go](https://github.com/kubernetes/client-go).

![Demo](docs/media/demo.gif)

## Code of Conduct

This project is for everyone. We ask that our users and contributors take a few minutes to review our [Code of Conduct](CODE_OF_CONDUCT.md).

## Documentation

Documentation is [available here](https://kube-burner.github.io/kube-burner/)

## Downloading Kube-burner

In case you want to start tinkering with Kube-burner now:

- You can find the binaries in the [releases section of the repository](https://github.com/kube-burner/kube-burner/releases).
- There's also a container image available at [quay](https://quay.io/repository/kube-burner/kube-burner?tab=tags).
- Example configuration files can be found at the [examples directory](./examples).

## Contributing Guidelines, CI, and Code Style

Please read the [Contributing section](https://kube-burner.github.io/kube-burner/latest/contributing/) before contributing to this project. It provides information on how to contribute, guidelines for setting an environment a CI checks to be done before commiting code.

This project utilizes a Continuous Integration (CI) pipeline to ensure code quality and maintain project standards. The CI process automatically builds, tests, and verifies the project on each commit and pull request.
