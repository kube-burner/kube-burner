#!/bin/bash -e

source base.sh

set -e
rc=0
uuid=$(uuidgen)

check_ns() {
  log "Checking the number of namespaces labeled with ${1} is ${2}"
  if [[ $(kubectl get ns -l ${1} -o name | wc -l) != ${2} ]]; then
    log "Number of namespaces labeled with ${1} less than expected"
    rc=1
  fi
}

check_destroyed_ns() {
  log "Checking namespace ${1} has been destroyed"
  if [[ $(kubectl get ns -l ${1} -o name | wc -l) != 0 ]]; then
    log "Namespaces labeled with ${1} not destroyed"
    rc=1
  fi
}

check_running_pods() {
  local running_pods=0
  local pods=0
  namespaces=$(kubectl get ns -l ${1} --no-headers | awk '{print $1}')
  for ns in ${namespaces}; do
    pods=$(kubectl get pod -n ${ns} | grep -c Running)
    running_pods=$((running_pods + pods))
  done
  if [[ ${running_pods} != ${2} ]]; then
    log "Running pods in namespaces labeled with ${1} different from expected"
    rc=1
  fi
}

check_files () {
  for f in collected-metrics/prometheusRSS.json collected-metrics/namespaced-podLatency.json collected-metrics/namespaced-podLatency-summary.json; do
    log "Checking file ${f}"
    if [[ ! -f $f ]]; then
      log "File ${f} not present"
      rc=1
    fi
  done
}

log "Running kube-burner init"
kube-burner init -c kube-burner.yml --uuid ${uuid} --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml -a alert-profile.yaml
check_files
check_ns kube-burner-job=not-namespaced,kube-burner-uuid=${uuid} 1
check_ns kube-burner-job=namespaced,kube-burner-uuid=${uuid} 5
check_running_pods kube-burner-job=namespaced,kube-burner-uuid=${uuid} 5 
log "Running kube-burner destroy"
kube-burner destroy --uuid ${uuid}
check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid=${uuid}
check_destroyed_ns kube-burner-job=namespaced,kube-burner-uuid=${uuid}
log "Evaluating alerts"
kube-burner check-alerts -u http://localhost:9090 -a alert-profile.yaml --start $(date -d "-2 minutes" +%s)
exit ${rc}
