#!/bin/bash
# vi: ft=bash

bold=$(tput bold)
normal=$(tput sgr0)

log() {
    echo "${bold}$(date '+%d-%m-%YT%H:%M:%S')" "${*}${normal}"
}

setup-kind() {
  log "Downloading kind"
  pushd "${TEMP_FOLDER}" || return
  curl -LsSO https://github.com/kubernetes-sigs/kind/releases/download/"${KIND_VERSION}"/kind-linux-amd64
  chmod +x kind-linux-amd64
  popd || return
  log "Deploying cluster"
  "${TEMP_FOLDER}"/kind-linux-amd64 create cluster --config kind.yml --image kindest/node:"${K8S_VERSION}" --name kind --wait 300s -v=1
}

destroy-kind() {
  log "Destroying kind server"
  "${TEMP_FOLDER}"/kind-linux-amd64 delete cluster
}

setup-prometheus() {
  log "Setting up prometheus instance"
  podman run --rm -d --name prometheus --network=host docker.io/prom/prometheus:latest
  sleep 10
}

check_ns() {
  log "Checking the number of namespaces labeled with \"${1}\" is \"${2}\""
  if [[ $(kubectl get ns -l "${1}" -o name | wc -l) != "${2}" ]]; then
    log "Number of namespaces labeled with \"${1}\" less than expected"
    return 1
  fi
}

check_destroyed_ns() {
  log "Checking namespace \"${1}\" has been destroyed"
  if [[ $(kubectl get ns -l "${1}" -o name | wc -l) != 0 ]]; then
    log "Namespaces labeled with \"${1}\" not destroyed"
    return 1
  fi
}

check_running_pods() {
  local running_pods=0
  local pods=0
  namespaces=$(kubectl get ns -l "${1}" --no-headers | awk '{print $1}')
  for ns in ${namespaces}; do
    pods=$(kubectl get pod -n "${ns}" | grep -c Running)
    running_pods=$((running_pods + pods))
  done
  if [[ "${running_pods}" != "${2}" ]]; then
    log "Running pods in namespaces labeled with \"${1}\" different from expected"
    return 1
  fi
}

check_files() {
  rc=0
  file_list="${TEMP_FOLDER}/collected-metrics/top2PrometheusCPU.json ${TEMP_FOLDER}/collected-metrics/prometheusRSS.json ${TEMP_FOLDER}/collected-metrics/prometheusRSS.json ${TEMP_FOLDER}/collected-metrics/podLatencyMeasurement-namespaced.json ${TEMP_FOLDER}/collected-metrics/podLatencyQuantilesMeasurement-namespaced.json"
  if [[ $LATENCY_ONLY == "true" ]]; then
    file_list="${TEMP_FOLDER}/collected-metrics/podLatencyMeasurement-namespaced.json ${TEMP_FOLDER}/collected-metrics/podLatencyQuantilesMeasurement-namespaced.json"
  fi
  for f in ${file_list}; do
    log "Checking file ${f}"
    if [[ ! -f $f ]]; then
      log "File ${f} not present"
      rc=$((rc + 1))
      continue
    fi
    if [[ $(jq . < "${f}" | wc -l) -le 1 ]]; then
      log "File \"${f}\" has a length of less thn 2 lines"
      cat "${f}"
    fi
  done
  return ${rc}
}

test_init_checks() {
  rc=0
  if [[ ${INDEXING} == "true" ]]; then
    check_files
  fi
  check_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}" 6
  rc=$((rc + $?))
  check_running_pods kube-burner-job=namespaced,kube-burner-uuid="${UUID}" 6
  rc=$((rc + $?))
  timeout 500 kube-burner init -c kube-burner-delete.yml --uuid "${UUID}" --log-level=debug
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  rc=$((rc + $?))
  log "Running kube-burner destroy"
  kube-burner destroy --uuid "${UUID}"
  check_destroyed_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}"
  rc=$((rc + $?))
  log "Evaluating alerts"
  kube-burner check-alerts -u http://localhost:9090 -a alert-profile.yaml --start "$(date -d '-2 minutes' +%s)"
  return ${rc}
}

print_events() {
  kubectl get events --sort-by='.lastTimestamp' -A
}

# shellcheck disable=SC2086
check_metric_value() {
  for metric in "${@}"; do
    endpoint="${ES_SERVER}/${ES_INDEX}/_search?q=uuid.keyword:${UUID}+AND+metricName.keyword:${metric}"
    RESULT=$(curl -sS ${endpoint} | jq '.hits.total.value // error')
    RETURN_CODE=$?
    if [ "${RETURN_CODE}" -ne 0 ]; then
      echo "Return code: ${RETURN_CODE}"
      return 1
    elif [ "${RESULT}" == 0 ]; then
      echo "$1 not found in ${endpoint}"
      return 1
    else
      return 0
    fi
  done
}
