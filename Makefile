
.PHONY: build clean test help


BIN_DIR = bin
BIN_NAME = kube-burner
CGO=0

GIT_COMMIT = $(shell git rev-parse HEAD)
GIT_BRANCH = $(shell git rev-parse --symbolic-full-name --abbrev-ref HEAD)
SOURCES := $(shell find . -type f -name "*.go")
BUILD_DATE = $(shell date '+%Y-%m-%d-%H:%M:%S')
KUBE_BURNER_PACKAGE = github.com/rsevilla87/kube-burner

all: build

help:
	@echo "Commands for $(BIN_NAME):"
	@echo
	@echo 'Usage:'
	@echo '    make build           Compile the project.'
	@echo '    make clean           Clean the directory tree.'
	@echo

build: $(BIN_DIR)/$(BIN_NAME)

$(BIN_DIR)/$(BIN_NAME): $(SOURCES)
	@echo "Building $(BIN_NAME)"
	@echo "GOPATH=$(GOPATH)"
	CGO_ENABLED=$(CGO) go build -v -mod vendor -ldflags "-X $(KUBE_BURNER_PACKAGE)/version.GitCommit=$(GIT_COMMIT) -X $(KUBE_BURNER_PACKAGE)/version.BuildDate=$(BUILD_DATE) -X $(KUBE_BURNER_PACKAGE)/version.Version=$(GIT_BRANCH)" -o $(BIN_DIR)/$(BIN_NAME)

lint:
	golangci-lint run

clean:
	test ! -e bin/$(BIN_NAME) || rm $(BIN_DIR)/$(BIN_NAME)

vendor:
	go mod vendor

install:
	cp $(BIN_DIR)/$(BIN_NAME) /usr/bin/$(BIN_NAME)
