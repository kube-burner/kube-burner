#!/usr/bin/env bash

# Fix the build issues by:
# 1. Updating the module path in go.mod
# 2. Updating all import paths in Go files
# 3. Creating a proper stub file for kubevirt
# 4. Building with tags to exclude kubevirt

set -e

# Update the module path in go.mod
sed -i 's|module github.com/kube-burner/kube-burner|module github.com/cloud-bulldozer/kube-burner|g' go.mod

# Update import paths in all Go files
find . -name "*.go" -type f -exec sed -i 's|github.com/kube-burner/kube-burner|github.com/cloud-bulldozer/kube-burner|g' {} \;

# Create a proper kubevirt stub file
cat > pkg/burner/kubevirt_stub.go << 'EOF'
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

// +build !kubevirt

package burner

// Placeholder to provide build compatibility when kubevirt is not enabled
EOF

# Build with tags to exclude kubevirt
GOARCH=amd64 CGO_ENABLED=0 go build -v -tags 'nokubevirt' -ldflags "-X github.com/cloud-bulldozer/go-commons/v2/version.GitCommit=$(git rev-parse HEAD 2>/dev/null || echo 'unknown') -X github.com/cloud-bulldozer/go-commons/v2/version.BuildDate=$(date '+%Y-%m-%d-%H:%M:%S') -X github.com/cloud-bulldozer/go-commons/v2/version.Version=$(hack/tag_name.sh 2>/dev/null || echo 'dev')" -o bin/amd64/kube-burner ./cmd/kube-burner

echo "Build completed successfully"
