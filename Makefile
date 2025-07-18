
.PHONY: build lint clean test help images push manifest manifest-build all


ARCH ?= $(shell uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)
BIN_NAME = kube-burner
BIN_DIR = bin
BIN_PATH = $(BIN_DIR)/$(ARCH)/$(BIN_NAME)
CGO = 0
TEST_BINARY ?= $(CURDIR)/$(BIN_PATH)

GIT_COMMIT = $(shell git rev-parse HEAD)
VERSION ?= $(shell hack/tag_name.sh)
SOURCES := $(shell find . -type f -name "*.go")
BUILD_DATE = $(shell date '+%Y-%m-%d-%H:%M:%S')
KUBE_BURNER_VERSION= github.com/cloud-bulldozer/go-commons/v2/version

# Containers
ENGINE ?= podman
REGISTRY = quay.io
ORG ?= kube-burner
CONTAINER_NAME = $(REGISTRY)/$(ORG)/kube-burner:$(VERSION)
CONTAINER_NAME_ARCH = $(REGISTRY)/$(ORG)/kube-burner:$(VERSION)-$(ARCH)
MANIFEST_ARCHS ?= amd64 arm64 ppc64le s390x

all: lint build images push

help:
	@echo "Commands for $(BIN_PATH):"
	@echo
	@echo 'Usage:'
	@echo '    make lint                     	Install and execute pre-commit'
	@echo '    make clean                    	Clean the compiled binaries'
	@echo '    [ARCH=arch] make build        	Compile the project for arch, default amd64'
	@echo '    [ARCH=arch] make build-release 	Compile hardened release binary, default amd64'
	@echo '    [ARCH=arch] make install      	Installs kube-burner binary in the system, default amd64'
	@echo '    [ARCH=arch] make images       	Build images for arch, default amd64'
	@echo '    [ARCH=arch] make push         	Push images for arch, default amd64'
	@echo '    make manifest                 	Create and push manifest for the different architectures supported'
	@echo '    make test                     	Run linting and all tests'
	@echo '    make test-k8s                 	Run Kubernetes tests'
	@echo '    make test-group-<group>       	Run specific test group (churn, gc, indexing, etc.)'
	@echo '    make help                     	Show this message'
	@echo
	@echo 'Test Groups:'
	@echo '    churn, gc, indexing, kubeconfig, kubevirt, alert, crd, delete,'
	@echo '    read, sequential, userdata, datavolume, metrics'
	@echo
	@echo 'Environment Variables:'
	@echo '    TEST_BINARY                   	Path to kube-burner binary for tests'
	@echo '    TEST_FILTER                   	Filter pattern for bats tests'
	@echo '    TEST_TIMEOUT                  	Timeout for individual tests (default: no timeout)'

build: $(BIN_PATH)

$(BIN_PATH): $(SOURCES)
	@echo -e "\033[2mBuilding $(BIN_PATH)\033[0m"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -v \
		-ldflags "-X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) \
		-X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) \
		-X $(KUBE_BURNER_VERSION).Version=$(VERSION)" \
		-o $(BIN_PATH) ./cmd/kube-burner

build-release: $(SOURCES)
	@echo -e "\033[2mBuilding hardened release binary $(BIN_PATH)\033[0m"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -v -trimpath \
		-ldflags "-s -w -X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) \
		-X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) \
		-X $(KUBE_BURNER_VERSION).Version=$(VERSION)" \
		-o $(BIN_PATH) ./cmd/kube-burner

lint:
	@echo "Executing pre-commit for all files"
	pre-commit run --all-files
	@echo "pre-commit executed."

clean:
	test ! -e $(BIN_DIR) || rm -Rf $(BIN_PATH)

install:
	cp $(BIN_PATH) /usr/bin/$(BIN_NAME)

images:
	@echo -e "\n\033[2mBuilding container $(CONTAINER_NAME_ARCH)\033[0m"
	$(ENGINE) build --arch=$(ARCH) -f Containerfile $(BIN_DIR)/$(ARCH)/ -t $(CONTAINER_NAME_ARCH)

push:
	@echo -e "\033[2mPushing container $(CONTAINER_NAME_ARCH)\033[0m"
	$(ENGINE) push $(CONTAINER_NAME_ARCH)

manifest: manifest-build
	@echo -e "\033[2mPushing container manifest $(CONTAINER_NAME)\033[0m"
	$(ENGINE) manifest push $(CONTAINER_NAME) $(CONTAINER_NAME)

manifest-build:
	@echo -e "\033[2mCreating container manifest $(CONTAINER_NAME)\033[0m"
	$(ENGINE) manifest create $(CONTAINER_NAME)
	for arch in $(MANIFEST_ARCHS); do \
		$(ENGINE) manifest add $(CONTAINER_NAME) $(CONTAINER_NAME)-$${arch}; \
	done

test: lint test-k8s

test-k8s:
	cd test && KUBE_BURNER=$(TEST_BINARY) bats $(if $(TEST_FILTER),--filter "$(TEST_FILTER)",) -F pretty -T --print-output-on-failure $(if $(TEST_TIMEOUT),--timeout $(TEST_TIMEOUT),) test-k8s.bats

# Test group mappings for parallel execution
test-group-churn:
	$(MAKE) test-k8s TEST_FILTER="churn=true"

test-group-gc:
	$(MAKE) test-k8s TEST_FILTER="gc=false"

test-group-indexing:
	$(MAKE) test-k8s TEST_FILTER="indexing=true|local-indexing=true"

test-group-kubeconfig:
	$(MAKE) test-k8s TEST_FILTER="kubeconfig"

test-group-kubevirt:
	$(MAKE) test-k8s TEST_FILTER="kubevirt|vm-latency"

test-group-alert:
	$(MAKE) test-k8s TEST_FILTER="check-alerts|alerting=true"

test-group-crd:
	$(MAKE) test-k8s TEST_FILTER="crd"

test-group-delete:
	$(MAKE) test-k8s TEST_FILTER="delete=true"

test-group-read:
	$(MAKE) test-k8s TEST_FILTER="read"

test-group-sequential:
	$(MAKE) test-k8s TEST_FILTER="sequential patch"

test-group-userdata:
	$(MAKE) test-k8s TEST_FILTER="user data file"

test-group-datavolume:
	$(MAKE) test-k8s TEST_FILTER="datavolume latency"

test-group-metrics:
	$(MAKE) test-k8s TEST_FILTER="metrics aggregation|metrics-endpoint=true"

# Validate test groups by listing available tests
validate-test-groups:
	@echo "Validating test groups..."
	@cd test && bats --list test-k8s.bats | grep -E "@test" || true
	@echo "Test group validation complete"

# Run a quick smoke test to ensure basic functionality
smoke-test:
	@echo "Running smoke test..."
	$(MAKE) test-group-gc TEST_BINARY=$(TEST_BINARY)
	@echo "Smoke test passed"
