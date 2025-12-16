#!/bin/bash
#
echo -e "FROM registry.fedoraproject.org/fedora-minimal:latest\nRUN microdnf install -y nmap-ncat procps-ng" | podman build --jobs=4 --platform=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x --manifest=quay.io/cloud-bulldozer/fedora-nc:latest -f - .
podman manifest push quay.io/cloud-bulldozer/fedora-nc:latest
