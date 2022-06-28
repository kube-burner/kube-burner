#!/bin/bash

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

print_events () {
  kubectl get events --sort-by='.lastTimestamp' -A
}
