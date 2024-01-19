
.PHONY: build lint clean test help images push manifest manifest-build all


ARCH ?= amd64
BIN_NAME = kube-burner
BIN_DIR = bin
BIN_PATH = $(BIN_DIR)/$(ARCH)/$(BIN_NAME)
CGO = 0

GIT_COMMIT = $(shell git rev-parse HEAD)
VERSION ?= $(shell hack/tag_name.sh)
SOURCES := $(shell find . -type f -name "*.go")
BUILD_DATE = $(shell date '+%Y-%m-%d-%H:%M:%S')
KUBE_BURNER_VERSION= github.com/cloud-bulldozer/go-commons/version

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
	@echo '    make lint                     Install and execute pre-commit'
	@echo '    make clean                    Clean the compiled binaries'
	@echo '    [ARCH=arch] make build        Compile the project for arch, default amd64'
	@echo '    [ARCH=arch] make install      Installs kube-burner binary in the system, default amd64'
	@echo '    [ARCH=arch] make images       Build images for arch, default amd64'
	@echo '    [ARCH=arch] make push         Push images for arch, default amd64'
	@echo '    make manifest                 Create and push manifest for the different architectures supported'
	@echo '    make help                     Show this message'

build: $(BIN_PATH)

$(BIN_PATH): $(SOURCES)
	@echo -e "\033[2mBuilding $(BIN_PATH)\033[0m"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -v -ldflags "-X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) -X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) -X $(KUBE_BURNER_VERSION).Version=$(VERSION)" -o $(BIN_PATH) ./cmd/kube-burner

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
	cd test && bats -F pretty -T --print-output-on-failure test-k8s.bats
