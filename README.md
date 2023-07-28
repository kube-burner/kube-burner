![Kube-burner Logo](./media/logo/kube-burner-logo.png)

[![Status](https://github.com/cloud-bulldozer/kube-burner/actions/workflows/pullrequest.yml/badge.svg?branch=master&event=push)](https://github.com/cloud-bulldozer/kube-burner/actions/workflows/pullrequest.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloud-bulldozer/kube-burner)](https://goreportcard.com/report/github.com/cloud-bulldozer/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# What is Kube-burner

Kube-burner is a Kubernetes performance and scale test orchestration framework. It provides multi-faceted functionality, the most important of which are summarized below.

- Create, delete and patch Kubernetes resources at scale.
- Prometheus metric collection and indexing.
- Measurements.
- Alerting.

Kube-burner is a binary application written in golang that makes extensive usage of the official k8s client library, [client-go](https://github.com/kubernetes/client-go).

![Demo](docs/media/demo.gif)

## Documentation

Documentation is [available here](https://cloud-bulldozer.github.io/kube-burner/)

## Downloading Kube-burner

In case you want to start tinkering with Kube-burner now:

- You can find the binaries in the [releases section of the repository](https://github.com/cloud-bulldozer/kube-burner/releases).
- There's also a container image available at [quay](https://quay.io/repository/cloud-bulldozer/kube-burner?tab=tags).
- Example configuration files can be found at the [examples directory](./examples).

## Contributing Guidelines, CI, and Code Style

Please read the [Contributing section](https://cloud-bulldozer.github.io/kube-burner/latest/contributing/) before contributing to this project. It provides information on how to contribute, guidelines for setting an environment a CI checks to be done before commiting code.

This project utilizes a Continuous Integration (CI) pipeline to ensure code quality and maintain project standards. The CI process automatically builds, tests, and verifies the project on each commit and pull request.
