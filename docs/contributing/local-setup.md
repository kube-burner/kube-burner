# Local Development Setup

This guide will help you set up a complete development environment for kube-burner, including all necessary tools and dependencies.

## Prerequisites

### 1. Go Installation

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

### 2. Container Runtime Installation

Install a container runtime for your platform. The project primarily uses Podman, but Docker is also supported:

**Fedora/RHEL/CentOS:**
```bash
# Install Podman (recommended)
sudo dnf install podman

# Or install Docker
sudo dnf install docker
sudo systemctl start docker
sudo systemctl enable docker
sudo usermod -aG docker $USER
```

**Ubuntu/Debian:**
```bash
# Install Podman
sudo apt-get update
sudo apt-get install podman

# Or install Docker
sudo apt-get install docker.io
sudo systemctl start docker
sudo systemctl enable docker
sudo usermod -aG docker $USER
```

**macOS:**
- Install [Podman Desktop](https://podman-desktop.io/) (recommended)
- Or install [Docker Desktop](https://www.docker.com/products/docker-desktop)

**Windows:**
- Install [Podman Desktop](https://podman-desktop.io/) with WSL2 backend (recommended)
- Or install [Docker Desktop](https://www.docker.com/products/docker-desktop) with WSL2 backend

### 3. Kind (Kubernetes in Docker)

Install Kind to create local Kubernetes clusters:

**Linux (x86_64):**
```bash
# Install Kind
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
chmod +x ./kind
# Install to ~/.local/bin (no sudo required)
mkdir -p ~/.local/bin
mv ./kind ~/.local/bin/
export PATH=$PATH:~/.local/bin

# Verify installation
kind version
```

**macOS (Intel):**
```bash
# Install Kind for Intel Macs
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-darwin-amd64
chmod +x ./kind
mkdir -p ~/.local/bin
mv ./kind ~/.local/bin/
export PATH=$PATH:~/.local/bin
```

**macOS (Apple Silicon/ARM):**
```bash
# Install Kind for Apple Silicon Macs
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-darwin-arm64
chmod +x ./kind
mkdir -p ~/.local/bin
mv ./kind ~/.local/bin/
export PATH=$PATH:~/.local/bin
```

**Note for macOS users**: Virtualization tests (KubeVirt) will fail on macOS due to virtualization limitations. Consider using Linux for full test coverage.

### 4. Kubectl Installation

Install kubectl to interact with Kubernetes clusters:

**Linux (x86_64):**
```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
mkdir -p ~/.local/bin
mv kubectl ~/.local/bin/
export PATH=$PATH:~/.local/bin
```

**macOS (Intel):**
```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/amd64/kubectl"
chmod +x kubectl
mkdir -p ~/.local/bin
mv kubectl ~/.local/bin/
export PATH=$PATH:~/.local/bin
```

**macOS (Apple Silicon/ARM):**
```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/arm64/kubectl"
chmod +x kubectl
mkdir -p ~/.local/bin
mv kubectl ~/.local/bin/
export PATH=$PATH:~/.local/bin
```

**Verify installation:**
```bash
kubectl version --client
```

### 5. Additional Tools

Install all additional tools in one go:

**Fedora/RHEL/CentOS:**
```bash
sudo dnf install make bats
```

**Ubuntu/Debian:**
```bash
sudo apt-get update && sudo apt-get install make bats
```

**macOS:**
```bash
brew install make bats-core
```

**Windows (WSL2):**
```bash
sudo apt-get update && sudo apt-get install make bats
```

### 6. Helm Installation (Optional)

Helm is required for some optional components like Prometheus and OpenSearch:

**Fedora/RHEL/CentOS:**
```bash
sudo dnf install helm
```

**Ubuntu/Debian:**
```bash
curl https://baltocdn.com/helm/signing.asc | gpg --dearmor | sudo tee /usr/share/keyrings/helm.gpg > /dev/null
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/helm.gpg] https://baltocdn.com/helm/stable/debian/ all main" | sudo tee /etc/apt/sources.list.d/helm-stable-debian.list
sudo apt-get update
sudo apt-get install helm
```

**macOS:**
```bash
brew install helm
```

**Windows (WSL2):**
```bash
curl https://baltocdn.com/helm/signing.asc | gpg --dearmor | sudo tee /usr/share/keyrings/helm.gpg > /dev/null
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/helm.gpg] https://baltocdn.com/helm/stable/debian/ all main" | sudo tee /etc/apt/sources.list.d/helm-stable-debian.list
sudo apt-get update
sudo apt-get install helm
```

## Setting Up a Local Kubernetes Cluster

### 1. Create a Kind Cluster

```bash
# Create a single-node cluster
kind create cluster --name kube-burner-test

# Verify cluster is running
kubectl cluster-info
kubectl get nodes
```

### 2. Install KubeVirt (Required for Virtualization Tests)

KubeVirt is required for running virtualization-related tests:

```bash
# Get the latest KubeVirt version
KUBEVIRT_VERSION=$(gh release view --repo kubevirt/kubevirt --json tagName -q '.tagName')

# Install KubeVirt
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml

# Wait for KubeVirt to be ready
kubectl -n kubevirt wait kv kubevirt --for condition=Available --timeout=300s
```

### 3. Install Prometheus (Optional, for metrics testing)

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

### 4. Install OpenSearch (Optional, for metrics indexing)

```bash
# Add OpenSearch Helm repository
helm repo add opensearch https://opensearch-project.github.io/helm-charts/
helm repo update

# Install OpenSearch
helm install opensearch opensearch/opensearch \
  --namespace monitoring \
  --create-namespace \
  --set clusterName=opensearch-cluster \
  --set globalNodeSelector."kubernetes\.io/os"=linux
```

## Building Kube-burner

### 1. Clone the Repository

```bash
git clone https://github.com/kube-burner/kube-burner.git
cd kube-burner
```

### 2. Build the Binary

```bash
# Standard development build
make build

# Or build for specific architecture
ARCH=amd64 make build

# Verify the binary
./bin/amd64/kube-burner version
```

### 3. Install Dependencies (for development)

```bash
# Install pre-commit hooks
pip install pre-commit
pre-commit install

# Install Go dependencies
go mod download
```

## Running Your First Test

### 1. Basic Health Check

```bash
# Check cluster health (uses default kubeconfig)
./bin/amd64/kube-burner health-check

# Alternative: specify custom kubeconfig
./bin/amd64/kube-burner health-check --kubeconfig ~/.kube/config

# Or set KUBECONFIG environment variable
export KUBECONFIG=~/.kube/config
./bin/amd64/kube-burner health-check
```

### 2. Run a Simple Workload

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

### 3. Run Built-in Examples

```bash
# Run cluster density test
cd examples/workloads/cluster-density
./bin/amd64/kube-burner init --config cluster-density.yml

# Run kubelet density test
cd ../kubelet-density
./bin/amd64/kube-burner init --config kubelet-density.yml
```

## Development Workflow

### 1. Running Tests

```bash
# Run all tests (includes lint and bats tests)
make test

# Run specific bats test
make test-k8s TEST_FILTER="test-name"

# Run linting only
make lint
```

### 2. Code Quality Checks

```bash
# Run pre-commit hooks
pre-commit run --all-files

# Run Go linting (this is what make lint executes)
golangci-lint run
```

### 3. Building for Different Platforms

```bash
# Build for multiple architectures
make build ARCH=amd64
make build ARCH=arm64

# Build hardened binary
make build-hardened
```

## Troubleshooting

### Common Issues

**1. WSL2 Issues (Windows):**
```bash
# Ensure WSL2 is enabled
wsl --set-default-version 2
wsl --set-version Ubuntu 2

# Check WSL version
wsl -l -v
```

**2. Container Runtime Permission Issues:**
```bash
# For Docker: Add user to docker group
sudo usermod -aG docker $USER
newgrp docker

# For Podman: No additional setup needed, runs rootless by default
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

**5. KubeVirt Issues:**
```bash
# Check KubeVirt status
kubectl get kubevirt -n kubevirt

# Check KubeVirt operator
kubectl get pods -n kubevirt

# Reinstall if needed
kubectl delete -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-cr.yaml
kubectl delete -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml
```

### Getting Help

- Check the [documentation](https://kube-burner.github.io/kube-burner/)
- Review [example configurations](../examples)
- Open an [issue](https://github.com/kube-burner/kube-burner/issues) for bugs
- Join discussions in [GitHub Discussions](https://github.com/kube-burner/kube-burner/discussions)
