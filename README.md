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

## Local Development Setup

This guide will help you set up a complete development environment for kube-burner, including all necessary tools and dependencies.

### Prerequisites

#### 1. Go Installation

Kube-burner requires Go 1.23.0 or later. Install Go from the [official website](https://golang.org/dl/):

**Linux/macOS:**
```bash
# Download and install Go
wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz

# Add to PATH (add to ~/.bashrc or ~/.zshrc)
export PATH=$PATH:/usr/local/go/bin

# Verify installation
go version
```

**Windows (WSL2):**
```bash
# Ensure WSL2 is enabled
wsl --set-version Ubuntu 2

# Install Go in WSL2
wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
```

#### 2. Docker Installation

Install Docker for your platform:

**Linux:**
```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install docker.io
sudo systemctl start docker
sudo systemctl enable docker
sudo usermod -aG docker $USER

# Log out and back in, or run:
newgrp docker
```

**macOS:**
- Download and install [Docker Desktop](https://www.docker.com/products/docker-desktop)

**Windows:**
- Install [Docker Desktop](https://www.docker.com/products/docker-desktop) with WSL2 backend

#### 3. Kind (Kubernetes in Docker)

Install Kind to create local Kubernetes clusters:

```bash
# Install Kind
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
chmod +x ./kind
sudo mv ./kind /usr/local/bin/kind

# Verify installation
kind version
```

#### 4. Kubectl Installation

Install kubectl to interact with Kubernetes clusters:

```bash
# Linux/macOS
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/

# Verify installation
kubectl version --client
```

#### 5. Additional Tools

**Make (for build commands):**
```bash
# Ubuntu/Debian
sudo apt-get install make

# macOS
brew install make

# Windows (WSL2)
sudo apt-get install make
```

**Bats (for testing):**
```bash
# Ubuntu/Debian
sudo apt-get install bats

# macOS
brew install bats-core

# Windows (WSL2)
sudo apt-get install bats
```

### Setting Up a Local Kubernetes Cluster

#### 1. Create a Kind Cluster

```bash
# Create a single-node cluster
kind create cluster --name kube-burner-test

# Verify cluster is running
kubectl cluster-info
kubectl get nodes
```

#### 2. Install Prometheus (Optional, for metrics testing)

```bash
# Add Prometheus Helm repository
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install Prometheus
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false
```

### Building Kube-burner

#### 1. Clone the Repository

```bash
git clone https://github.com/kube-burner/kube-burner.git
cd kube-burner
```

#### 2. Build the Binary

```bash
# Standard development build
make build

# Or build for specific architecture
ARCH=amd64 make build

# Verify the binary
./bin/amd64/kube-burner version
```

#### 3. Install Dependencies (for development)

```bash
# Install pre-commit hooks
pip install pre-commit
pre-commit install

# Install Go dependencies
go mod download
```

### Running Your First Test

#### 1. Basic Health Check

```bash
# Check cluster health
./bin/amd64/kube-burner health-check --kubeconfig ~/.kube/config
```

#### 2. Run a Simple Workload

Create a test configuration file `test-config.yml`:

```yaml
global:
  measurements:
    - name: podLatency
  clusterHealth: true

jobs:
  - name: simple-test
    jobIterations: 1
    qps: 10
    burst: 10
    namespace: kube-burner-test
    objects:
      - objectTemplate: |
          apiVersion: v1
          kind: Pod
          metadata:
            name: test-pod-{{ .Replica }}
            labels:
              app: test
          spec:
            containers:
            - name: nginx
              image: nginx:alpine
              ports:
              - containerPort: 80
        replicas: 5
```

Run the test:

```bash
./bin/amd64/kube-burner init --config test-config.yml
```

#### 3. Run Built-in Examples

```bash
# Run cluster density test
./bin/amd64/kube-burner init --config examples/workloads/cluster-density/cluster-density.yml

# Run kubelet density test
./bin/amd64/kube-burner init --config examples/workloads/kubelet-density/kubelet-density.yml
```

### Development Workflow

#### 1. Running Tests

```bash
# Run all tests
make test

# Run specific test
make test-k8s TEST_FILTER="test-name"

# Run linting
make lint
```

#### 2. Code Quality Checks

```bash
# Run pre-commit hooks
pre-commit run --all-files

# Run Go linting
golangci-lint run
```

#### 3. Building for Different Platforms

```bash
# Build for multiple architectures
make build ARCH=amd64
make build ARCH=arm64

# Build hardened binary
make build-hardened
```

### Troubleshooting

#### Common Issues

**1. WSL2 Issues (Windows):**
```bash
# Ensure WSL2 is enabled
wsl --set-default-version 2
wsl --set-version Ubuntu 2

# Check WSL version
wsl -l -v
```

**2. Docker Permission Issues:**
```bash
# Add user to docker group
sudo usermod -aG docker $USER
newgrp docker

# Or run docker with sudo (not recommended for development)
sudo docker ps
```

**3. Kind Cluster Issues:**
```bash
# Delete and recreate cluster
kind delete cluster --name kube-burner-test
kind create cluster --name kube-burner-test

# Check cluster status
kind get clusters
kubectl cluster-info
```

**4. Go Module Issues:**
```bash
# Clean module cache
go clean -modcache
go mod download
go mod tidy
```

#### Getting Help

- Check the [documentation](https://kube-burner.github.io/kube-burner/)
- Review [example configurations](./examples)
- Open an [issue](https://github.com/kube-burner/kube-burner/issues) for bugs
- Join discussions in [GitHub Discussions](https://github.com/kube-burner/kube-burner/discussions)

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
