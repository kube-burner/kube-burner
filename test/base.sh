#!/bin/bash

trap print_events ERR
export QPS=5
export BURST=10
export TERM=screen-256color
export JOB_ITERATIONS=4

bold=$(tput bold)
normal=$(tput sgr0)

log() {
    echo ${bold}$(date "+%d-%m-%YT%H:%M:%S") ${@}${normal}
}

print_events () {
  kubectl get events --sort-by='.lastTimestamp' -A
}
