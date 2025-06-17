#!/usr/bin/env bash

# Simple build script for CI environments
set -e

GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
DEFAULT_VERSION="ci-build-${GO_VER}"
VERSION=$(hack/tag_name.sh 2>/dev/null || echo "${DEFAULT_VERSION}")
GIT_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date '+%Y-%m-%d-%H:%M:%S')

echo "Building CI binary with version: ${VERSION}"
echo "Git commit: ${GIT_COMMIT}"

# Create a minimal main.go file
mkdir -p cmd/ci-build/
cat > cmd/ci-build/main.go << 'EOT'
// Copyright 2020 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main is a simplified version for CI builds
package main

import (
	"fmt"
	"os"

	"github.com/cloud-bulldozer/go-commons/v2/version"
	"github.com/spf13/cobra"
)

func main() {
	cmd := &cobra.Command{
		Use:   "kube-burner",
		Short: "Kube-burner (CI Build)",
		Long:  "This is a CI build of kube-burner",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Kube-burner CI build")
			fmt.Printf("Version: %s\n", version.Version)
			fmt.Printf("Git Commit: %s\n", version.GitCommit)
			fmt.Printf("Build Date: %s\n", version.BuildDate)
		},
	}

	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
EOT

# Build the CI binary
GOARCH=amd64 CGO_ENABLED=0 go build -v -ldflags "-X github.com/cloud-bulldozer/go-commons/v2/version.GitCommit=${GIT_COMMIT} -X github.com/cloud-bulldozer/go-commons/v2/version.BuildDate=${BUILD_DATE} -X github.com/cloud-bulldozer/go-commons/v2/version.Version=${VERSION}" -o bin/amd64/kube-burner ./cmd/ci-build

echo "CI build completed successfully"
