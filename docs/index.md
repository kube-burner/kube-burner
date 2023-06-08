[![Status](https://github.com/cloud-bulldozer/kube-burner/actions/workflows/pullrequest.yml/badge.svg?branch=master&event=push)](https://github.com/cloud-bulldozer/kube-burner/actions/workflows/pullrequest.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloud-bulldozer/kube-burner)](https://goreportcard.com/report/github.com/cloud-bulldozer/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# What is Kube-burner

Kube-burner is a Kubernetes performance toolset. It provides multiple functionalities where the most hightliged can be summarized in:

- Create, delete and patch Kubernetes at scale.
- Prometheus metric collection and indexing.
- Measurements.
- Alerting.

Kube-burner is a binary application written in golang that makes an intensive usage of the official k8s client library, [client-go](https://github.com/kubernetes/client-go).

![Demo](media/demo.gif)

# Quick starting with Kube-burner

In case you want to start tinkering with Kube-burner now:

- Find binaries for different CPU architectures and operating systems in the [releases section of the repository](https://github.com/cloud-bulldozer/kube-burner/releases).
- There's also a container image repository available at [quay](https://quay.io/repository/cloud-bulldozer/kube-burner?tab=tags).
- Some valid examples of configuration files, metrics profiles and grafana dashboards can be found in the [examples directory](https://github.com/cloud-bulldozer/kube-burner/tree/master/examples) of the repository.
