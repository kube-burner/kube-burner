#!/bin/bash
# vi: ft=bash
# shellcheck disable=SC2086

setup-kind() {
  KIND_FOLDER=$(mktemp -d)
  echo "Downloading kind"
  curl -LsS https://github.com/kubernetes-sigs/kind/releases/download/"${KIND_VERSION}"/kind-linux-amd64 -o ${KIND_FOLDER}/kind-linux-amd64
  chmod +x ${KIND_FOLDER}/kind-linux-amd64
  echo "Deploying cluster"
  ${KIND_FOLDER}/kind-linux-amd64 create cluster --config kind.yml --image kindest/node:"${K8S_VERSION}" --name kind --wait 300s -v=1
}

destroy-kind() {
  echo "Destroying kind server"
  "${KIND_FOLDER}"/kind-linux-amd64 delete cluster
}

setup-prometheus() {
  echo "Setting up prometheus instance"
  podman run --rm -d --name prometheus --network=host docker.io/prom/prometheus:latest
  sleep 10
}

check_ns() {
  echo "Checking the number of namespaces labeled with \"${1}\" is \"${2}\""
  if [[ $(kubectl get ns -l "${1}" -o name | wc -l) != "${2}" ]]; then
    echo "Number of namespaces labeled with \"${1}\" less than expected"
    return 1
  fi
}

check_destroyed_ns() {
  echo "Checking namespace \"${1}\" has been destroyed"
  if [[ $(kubectl get ns -l "${1}" -o name | wc -l) != 0 ]]; then
    echo "Namespaces labeled with \"${1}\" not destroyed"
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
    echo "Running pods in namespaces labeled with \"${1}\" different from expected"
    return 1
  fi
}

check_files() {
  rc=0
  file_list="${TEMP_FOLDER}/top2PrometheusCPU.json ${TEMP_FOLDER}/prometheusRSS.json ${TEMP_FOLDER}/podLatencyMeasurement-namespaced.json ${TEMP_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json"
  if [[ $LATENCY == "true" ]]; then
    file_list="${TEMP_FOLDER}/podLatencyMeasurement-namespaced.json ${TEMP_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json"
  fi
  if [[ $ALERTING == "true" ]]; then
    file_list=" ${TEMP_FOLDER}/alert.json"
  fi
  for f in ${file_list}; do
    echo "Checking file ${f}"
    if [[ ! -f $f ]]; then
      echo "File ${f} not present"
      rc=$((rc + 1))
      continue
    fi
    if [[ $(jq .[0].metricName ${f}) == "" ]]; then
      echo "Incorrect format in ${f}"
      cat "${f}"
    fi
  done
  if [[ ${rc} != 0 ]]; then
    echo "Content of ${TEMP_FOLDER}"
    ls ${TEMP_FOLDER}
  fi
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
  echo "Running kube-burner destroy"
  kube-burner destroy --uuid "${UUID}"
  check_destroyed_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}"
  rc=$((rc + $?))
  echo "Evaluating alerts"
  kube-burner check-alerts -u http://localhost:9090 -a alert-profile.yaml --start "$(date -d '-2 minutes' +%s)"
  return ${rc}
}

print_events() {
  kubectl get events --sort-by='.lastTimestamp' -A
}

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
