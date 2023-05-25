#!/bin/bash

trap print_events ERR
export QPS=2
export BURST=2
export TERM=screen-256color
export JOB_ITERATIONS=5

bold=$(tput bold)
normal=$(tput sgr0)

log() {
    echo "${bold}$(date '+%d-%m-%YT%H:%M:%S')" "${*}${normal}"
}

print_events() {
  kubectl get events --sort-by='.lastTimestamp' -A
}

setup-kind() {
  log "Downloading kind"
  curl -LsSO https://github.com/kubernetes-sigs/kind/releases/download/"${KIND_VERSION}"/kind-linux-amd64
  chmod +x kind-linux-amd64
  log "Deploying cluster"
  ./kind-linux-amd64 create cluster --config kind.yml --image kindest/node:"${K8S_VERSION}" --name kind --wait 300s -v=1
}

setup-prometheus() {
  log "Setting up prometheus instance"
  curl -sSL https://github.com/prometheus/prometheus/releases/download/v2.22.0/prometheus-2.22.0.linux-amd64.tar.gz | tar xz
  pushd prometheus-2.22.0.linux-amd64 || return
  ./prometheus --storage.tsdb.path=/tmp/promdata 2>/dev/null &
  sleep 10
  popd || return
}
