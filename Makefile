
.PHONY: build build-release build-hardened build-hardened-cgo lint clean test help images push manifest manifest-build all


ARCH ?= $(shell uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)
BIN_NAME = kube-burner
BIN_DIR = bin
BIN_PATH = $(BIN_DIR)/$(ARCH)/$(BIN_NAME)
CGO = 0
TEST_BINARY ?= $(CURDIR)/$(BIN_PATH)
# Security hardening flags for static builds
HARDENED_FLAGS = -buildmode=pie -tags=netgo,osusergo,static_build
HARDENED_LDFLAGS = -extldflags '-static'

# Full security hardening flags with CGO
HARDENED_CGO_FLAGS = -buildmode=pie
HARDENED_CGO_LDFLAGS = -linkmode=external -extldflags "-Wl,-z,relro,-z,now"

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

# Bats
JOBS ?= $(shell nproc)
FILTER_TAGS = $(shell hack/get_changed_labels.py hack/bats_test_mappings.yml)

all: lint build images push

help:
	@echo "Commands for $(BIN_PATH):"
	@echo
	@echo 'Usage:'
	@echo '    make lint                     		Install and execute pre-commit'
	@echo '    make clean                    		Clean the compiled binaries'
	@echo '    [ARCH=arch] make build        		Compile the project for arch, default amd64'
	@echo '    [ARCH=arch] make build-release 		Compile release binary, default amd64'
	@echo '    [ARCH=arch] make build-hardened 		Build with security hardening (static)'
	@echo '    [ARCH=arch] make build-hardened-cgo 	Build with full security hardening (requires glibc)'
	@echo '    [ARCH=arch] make install      		Installs kube-burner binary in the system, default amd64'
	@echo '    [ARCH=arch] make images       		Build images for arch, default amd64'
	@echo '    [ARCH=arch] make push         		Push images for arch, default amd64'
	@echo '    make manifest                 		Create and push manifest for the different architectures supported'
	@echo '    make help                     		Show this message'

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
	@echo -e "\033[2mBuilding secure release binary $(BIN_PATH)\033[0m"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -v -trimpath $(HARDENED_FLAGS) \
		-ldflags "-s -w $(HARDENED_LDFLAGS) -X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) \
		-X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) \
		-X $(KUBE_BURNER_VERSION).Version=$(VERSION)" \
		-o $(BIN_PATH) ./cmd/kube-burner

build-hardened: $(SOURCES)
	@echo -e "\033[2mBuilding hardened static $(BIN_PATH)\033[0m"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -v -trimpath $(HARDENED_FLAGS) \
		-ldflags "-s -w $(HARDENED_LDFLAGS) -X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) \
		-X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) \
		-X $(KUBE_BURNER_VERSION).Version=$(VERSION)" \
		-o $(BIN_PATH) ./cmd/kube-burner

build-hardened-cgo: $(SOURCES)
	@echo -e "\033[2mBuilding fully hardened $(BIN_PATH) with CGO\033[0m"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=1 go build -v -trimpath $(HARDENED_CGO_FLAGS) \
		-ldflags "-s -w $(HARDENED_CGO_LDFLAGS) -X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) \
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
	cd test && KUBE_BURNER=$(TEST_BINARY) bats $(if $(TEST_FILTER),--filter "$(TEST_FILTER)",) -F pretty -T --print-output-on-failure test-k8s.bats -j $(JOBS) $(FILTER_TAGS)
