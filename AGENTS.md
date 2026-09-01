# Agents

This file provides context for AI coding agents working on this project.

## Project overview

Kube-burner is a Kubernetes performance and scale test orchestration tool written in Go. It creates, patches, and deletes Kubernetes resources at scale, collects Prometheus metrics, takes latency measurements, and evaluates alerting rules. It is used to stress-test Kubernetes clusters and benchmark their performance.

Module path: `github.com/kube-burner/kube-burner/v2`

## Repository layout

```
cmd/kube-burner/       Single binary entrypoint (cobra CLI)
pkg/
  alerting/            Prometheus alert evaluation
  burner/              Core workload execution engine (create, delete, patch jobs)
  config/              Configuration parsing and types
  errors/              Custom error handling
  measurements/        Measurements (pod, node, job, netpol, service, datavolume, etc.)
  prometheus/          Prometheus client and metric scraping
  util/                Logging, cluster health, file utilities, metrics helpers
  watchers/            Kubernetes resource watchers
  workloads/           Workload registration and management
examples/
  workloads/           Example benchmark configurations
  metrics-profiles/    Example Prometheus metric profiles
  grafana-dashboards/  Example Grafana dashboards
test/                  Integration tests (bats framework)
hack/                  Build and CI helper scripts
docs/                  Documentation site (mkdocs)
```

## Building and testing

```sh
make build              # Development build -> bin/<arch>/kube-burner
make build-release      # Optimized release build with hardening
make lint               # Run pre-commit (golangci-lint, markdownlint, shellcheck)
make test               # Lint + integration tests
make test-k8s           # Integration tests only (requires a running cluster + kind)
```

Unit tests are colocated with source files (`*_test.go`). Integration tests use [bats](https://github.com/bats-core/bats-core) under `test/` and require a built binary and a running Kubernetes cluster (typically kind).

## CLI commands

The binary exposes these subcommands via cobra:

- `init` — Run a benchmark from a config file
- `destroy` — Delete benchmark resources
- `measure` — Take measurements without running a workload
- `index` — Scrape and index Prometheus metrics
- `check-alerts` — Evaluate alert rules for a time range
- `import` — Import a metrics tarball into an indexer
- `health-check` — Check cluster health
- `completion` — Generate bash completions

## Code conventions

- Logging: `github.com/sirupsen/logrus` (imported as `log`)
- CLI framework: `github.com/spf13/cobra`
- Kubernetes client: `k8s.io/client-go`
- YAML config: `gopkg.in/yaml.v3`
- Testing: `ginkgo/v2` + `gomega` for unit tests, bats for integration tests

## Linting

Pre-commit hooks run golangci-lint (v2), markdownlint, shellcheck, and check-json. The golangci-lint configuration is in `.golangci.yml` and enables: dupl, goconst, gocyclo, govet, ineffassign, misspell, nakedret, staticcheck, unconvert, unparam, unused. Formatters: gofmt, goimports.

## CI

GitHub Actions workflows:

- `linters.yml` — Pre-commit linting
- `ci-tests.yml` — Build and unit tests
- `test-k8s.yml` / `test-k8s-ppc64le.yml` — Integration tests on kind clusters
- `release.yml` / `gorelease.yml` — Release builds
- `builders.yml` / `image-upload.yml` — Container image builds
- `docs.yml` — Documentation site deployment
- `codeql.yml` — Security analysis
- `check-docs-links.yml` — Verify documentation links

## Container images

Built with `Containerfile` (Fedora minimal base). Multi-arch support: amd64, arm64, ppc64le, s390x. Container engine defaults to podman. Published to `quay.io/kube-burner/kube-burner`.

## Key dependencies

- `github.com/cloud-bulldozer/go-commons/v2` — Shared indexer and version utilities
- `github.com/kedacore/keda/v2` — KEDA ScaledObject types
- `kubevirt.io/api`, `kubevirt.io/client-go` — KubeVirt VM support
- `github.com/prometheus/common`, `github.com/prometheus/prometheus` — Prometheus querying
- `gonum.org/v1/gonum` — Statistical calculations for measurements

## Important patterns

- Configuration is YAML-based with Go template rendering (sprig functions supported). User data files can parameterize configs.
- Workloads define Kubernetes object templates that get rendered per-iteration with template variables.
- The `burner` package is the core execution engine: it runs jobs (create/delete/patch/read) with configurable parallelism, rate limiting, and wait conditions.
- Measurements are pluggable — registered by type and started/stopped around job execution. They produce metrics that can be indexed.
- Metrics indexing supports local JSON files, Elasticsearch, and Prometheus TSDB.
