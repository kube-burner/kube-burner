<img src='./media/horizontal/kube-burner-horizontal-color.png' width='65%'>

[![Go Report Card](https://goreportcard.com/badge/github.com/kube-burner/kube-burner)](https://goreportcard.com/report/github.com/kube-burner/kube-burner)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/8264/badge)](https://www.bestpractices.dev/projects/8264)

# What is Kube-burner

Kube-burner is a Kubernetes performance and scale test orchestration toolset. It provides multi-faceted functionality, the most important of which are summarized below.

- Create, delete and patch Kubernetes resources at scale.
- Prometheus metric collection and indexing.
- Measurements.
- Alerting.

Kube-burner is a binary application written in golang that makes extensive usage of the official k8s client library, [client-go](https://github.com/kubernetes/client-go).

![Demo](docs/media/demo.gif)

## Code of Conduct

This project is for everyone. We ask that our users and contributors take a few minutes to review our [Code of Conduct](./code-of-conduct.md).

## Documentation

Documentation is [available here](https://kube-burner.github.io/kube-burner/)

## Downloading Kube-burner

In case you want to start tinkering with Kube-burner now:

- You can find the binaries in the [releases section of the repository](https://github.com/kube-burner/kube-burner/releases).
- There's also a container image available at [quay](https://quay.io/repository/kube-burner/kube-burner?tab=tags).
- Example configuration files can be found at the [examples directory](./examples).

## Building from Source

Kube-burner provides multiple build options:

- `make build`              - Standard build for development
- `make build-release`      - Optimized release build  
- `make build-hardened`     - Security-hardened static binary
- `make build-hardened-cgo` - Full security hardening with CGO (requires glibc)

The default builds produce static binaries that work across all Linux distributions. The CGO-hardened build offers additional security features but requires glibc to be present on the target system.

## Contributing Guidelines, CI, and Code Style

Please read the [Contributing section](https://kube-burner.github.io/kube-burner/latest/contributing/) before contributing to this project. It provides information on how to contribute, guidelines for setting an environment a CI checks to be done before commiting code.

This project utilizes a Continuous Integration (CI) pipeline to ensure code quality and maintain project standards. The CI process automatically builds, tests, and verifies the project on each commit and pull request.

---

### Thanks to the contributors of Kube-burner ❤️

<div align="center">
<a href="https://github.com/kube-burner/kube-burner/graphs/contributors">
<img src="https://contrib.rocks/image?repo=kube-burner/kube-burner" alt="Kube-burner Contributors"/>
</a>
</div>

---

**We are a [Cloud Native Computing Foundation](https://cncf.io/) Sandbox Project.**

<img src="https://www.cncf.io/wp-content/uploads/2022/07/cncf-color-bg.svg" width=300 />

## License

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fkube-burner%2Fkube-burner.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fkube-burner%2Fkube-burner?ref=badge_large)

<br/>

