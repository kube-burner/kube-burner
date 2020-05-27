
.PHONY: build clean test help


BIN_DIR = bin
BIN_NAME = kube-burner
CGO=0

VERSION := $(shell grep "const Version " version/version.go | sed -E 's/.*"(.+)"$$/\1/')
GIT_COMMIT = $(shell git rev-parse HEAD)
GIT_DIRTY = $(shell test -n "`git status --porcelain`" && echo "+CHANGES" || true)
SOURCES := $(shell find . -type f -name "*.go")
BUILD_DATE = $(shell date '+%Y-%m-%d-%H:%M:%S')

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
	@echo "building ${BIN_NAME} ${VERSION}"
	@echo "GOPATH=${GOPATH}"
	CGO_ENABLED=$(CGO) go build -ldflags "-X github.com/rsevilla87/kube-burner/version.GitCommit=${GIT_COMMIT}${GIT_DIRTY} -X github.com/rsevilla87/kube-burner/version.BuildDate=${BUILD_DATE}" -o $(BIN_DIR)/${BIN_NAME}

clean:
	@test ! -e bin/${BIN_NAME} || rm $(BIN_DIR)/${BIN_NAME}
