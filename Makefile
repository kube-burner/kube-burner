
.PHONY: build clean test help image container push manifest


PLATFORMS ?= linux/amd64,linux/arm64,linux/ppc64le,linux/s390x
BIN_NAME = kube-burner
BIN_DIR = bin
BIN_PATH = $(BIN_DIR)/$(BIN_NAME)
CGO = 0

GIT_COMMIT = $(shell git rev-parse HEAD)
VERSION ?= $(shell hack/tag_name.sh)
SOURCES := $(shell find . -type f -name "*.go")
BUILD_DATE = $(shell date '+%Y-%m-%d-%H:%M:%S')
KUBE_BURNER_VERSION= github.com/cloud-bulldozer/kube-burner/pkg/version

# Containers
ENGINE ?= podman
REGISTRY = quay.io
ORG ?= cloud-bulldozer
CONTAINER_NAME = $(REGISTRY)/$(ORG)/kube-burner:$(VERSION)

all: lint build
image: container push

help:
	@echo "Commands for $(BIN_PATH):"
	@echo
	@echo 'Usage:'
	@echo '    make clean                    Clean the compiled binaries'
	@echo '    make build                    Compile the project for arch, default amd64'
	@echo '    make install                  Installs kube-burner binary in the system, default amd64'
	@echo '    make images                   Build images for arch, default amd64'
	@echo '    make push                     Push images for arch, default amd64'
	@echo '    make manifest                 Create and push manifest for the different architectures supported'
	@echo '    make help                     Show this message'

build: $(BIN_PATH)

$(BIN_PATH): $(SOURCES)
	@echo -e "\033[2mBuilding $(BIN_PATH)\033[0m"
	@echo "GOPATH=$(GOPATH)"
	CGO_ENABLED=$(CGO) go build -v -mod vendor -ldflags "-X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) -X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) -X $(KUBE_BURNER_VERSION).Version=$(VERSION)" -o $(BIN_PATH) ./cmd/kube-burner

lint:
	golangci-lint run

clean:
	test ! -e $(BIN_DIR) || rm -Rf $(BIN_PATH)

vendor:
	go mod vendor

deps-update:
	go mod tidy
	go mod vendor

install:
	cp $(BIN_PATH) /usr/bin/$(BIN_NAME)

container:
	@echo -e "\n\033[2mBuilding container $(CONTAINER_NAME)\033[0m"
	$(ENGINE) build -f Containerfile . -t=$(CONTAINER_NAME)

push:
	@echo -e "\033[2mPushing container $(CONTAINER_NAME)\033[0m"
	$(ENGINE) push $(CONTAINER_NAME)

manifest:
	@echo -e "\033[2mCreating container manifest $(CONTAINER_NAME)\033[0m"
	$(ENGINE) build --platform=$(PLATFORMS) -f Containerfile --manifest=$(CONTAINER_NAME)
	$(ENGINE) manifest push $(CONTAINER_NAME) $(CONTAINER_NAME)
