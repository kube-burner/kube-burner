<img src="./media/logo/kube-burner-logo.png" width="60%">

[![Build Status](https://github.com/cloud-bulldozer/kube-burner/workflows/Go/badge.svg?branch=master)](https://github.com/cloud-bulldozer/kube-burner/actions?query=workflow%3AGo)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloud-bulldozer/kube-burner)](https://goreportcard.com/report/github.com/cloud-bulldozer/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# What is Kube-burner?

Kube-burner is a tool aimed at stressing kubernetes clusters. The main functionallity it provides can be summarized in these three steps:

- Create/delete the objects declared in the jobs.
- Collect desired on-cluster prometheus metrics.
- Write and/or index them to the configured TSDB.

But it also provides other features like

- Standalone metrics collection
- Alerting
- PProf collection
- Pod startup latency measurements

[![asciicast](https://asciinema.org/a/KksoK5voK3al1FuOza89t1JAp.svg)](https://asciinema.org/a/KksoK5voK3al1FuOza89t1JAp)

## Documentation

Documentation is available at https://cloud-bulldozer.github.io/kube-burner/

## Downloading Kube-burner

In case you want to start tinkering with Kube-burner now:

- You can find the binaries in the [releases section of the repository](https://github.com/cloud-bulldozer/kube-burner/releases).
- There's also a container image available at [quay](https://quay.io/repository/cloud-bulldozer/kube-burner?tab=tags).
- Example configuration files can be found at the [examples directory](./examples).
