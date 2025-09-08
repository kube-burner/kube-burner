#!/usr/bin/env bash

TOP_DIR=$(dirname $(dirname $(realpath $0)))

ARCH=$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)
KIND_FOLDER=${KIND_FOLDER:-$(mktemp -d)}
KIND_YAML=${TOP_DIR}/test/k8s/kind.yml
KUBEVIRT_CR=${TOP_DIR}/test/k8s/objectTemplates/kubevirt-cr.yaml
source ${TOP_DIR}/test/helpers.bash

setup-kind

echo "Kind cluster created successfully"
echo "To cleanup the cluster run:"
echo "  ${KIND_FOLDER}/kind-linux-${ARCH} delete cluster"