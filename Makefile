
.PHONY: build clean test help images push


ARCH ?= amd64
BIN_DIR = bin
BIN_PATH = $(BIN_DIR)/$(ARCH)/kube-burner
CGO = 0

GIT_COMMIT = $(shell git rev-parse HEAD)
VERSION = $(shell git rev-parse --symbolic-full-name --abbrev-ref HEAD)
SOURCES := $(shell find . -type f -name "*.go")
BUILD_DATE = $(shell date '+%Y-%m-%d-%H:%M:%S')
KUBE_BURNER_VERSION= github.com/cloud-bulldozer/kube-burner/pkg/version

# Containers
ifeq (, $(shell command -v docker))
  ENGINE := podman
else
  ENGINE := docker
endif

REGISTRY = quay.io
ORG = cloud-bulldozer
CONTAINER_NAME = $(REGISTRY)/$(ORG)/kube-burner:$(VERSION)-$(ARCH)

all: lint build images push

help:
	@echo "Commands for $(BIN_PATH):"
	@echo
	@echo 'Usage:'
	@echo '    make clean                    Clean the directory tree'
	@echo '    [ARCH=arch] make build        Compile the project for arch, default amd64'
	@echo '    [ARCH=arch] make images       Build images for arch, default amd64'
	@echo '    [ARCH=arch] make push         Push images for arch, default amd64'
	@echo '    make help                     Show this message'

build: $(BIN_PATH)

$(BIN_PATH): $(SOURCES)
	@echo "Building $(BIN_PATH)"
	@echo "GOPATH=$(GOPATH)"
	GOARCH=$(ARCH) CGO_ENABLED=$(CGO) go build -v -mod vendor -ldflags "-X $(KUBE_BURNER_VERSION).GitCommit=$(GIT_COMMIT) -X $(KUBE_BURNER_VERSION).BuildDate=$(BUILD_DATE) -X $(KUBE_BURNER_VERSION).Version=$(VERSION)" -o $(BIN_PATH) ./cmd/kube-burner

lint:
	golangci-lint run

clean:
	test ! -e bin/$(BIN_PATH) || rm $(BIN_PATH)

vendor:
	go mod vendor

install:
	cp $(BIN_PATH) /usr/bin/$(BIN_PATH)

images:
	@echo -e "\n\033[2mBuilding container $(CONTAINER_NAME)\033[0m"
	$(ENGINE) build --arch=$(ARCH) -f Containerfile $(BIN_DIR)/$(ARCH)/ -t $(CONTAINER_NAME)

push:
	@echo -e "\n\033[2mPushing container $(CONTAINER_NAME)\033[0m"
	$(ENGINE) push $(CONTAINER_NAME)
