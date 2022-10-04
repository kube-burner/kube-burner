#!/bin/bash

./setup-kind.sh

trap print_events ERR
export QPS=2
export BURST=2
export TERM=screen-256color
export JOB_ITERATIONS=9

bold=$(tput bold)
normal=$(tput sgr0)

log() {
    echo ${bold}$(date "+%d-%m-%YT%H:%M:%S") ${@}${normal}
}

print_events() {
  kubectl get events --sort-by='.lastTimestamp' -A
}

setup-kind(){
  echo "Downloading kind"
  curl -LsSO https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-amd64
  chmod +x kind-linux-amd64
  echo "Deploying cluster"
  ./kind-linux-amd64 create cluster --config kind.yml --image kindest/node:${K8S_VERSION} --name kind --wait 300s -v=1
}
