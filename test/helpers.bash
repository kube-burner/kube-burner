#!/bin/bash
# vi: ft=bash
# shellcheck disable=SC2086,SC2068

KIND_VERSION=${KIND_VERSION:-v0.19.0}
K8S_VERSION=${K8S_VERSION:-v1.27.0}
OCI_BIN=${OCI_BIN:-podman}

setup-kind() {
  KIND_FOLDER=$(mktemp -d)
  echo "Downloading kind"
  curl -LsS https://github.com/kubernetes-sigs/kind/releases/download/"${KIND_VERSION}"/kind-linux-amd64 -o ${KIND_FOLDER}/kind-linux-amd64
  chmod +x ${KIND_FOLDER}/kind-linux-amd64
  echo "Deploying cluster"
  ${KIND_FOLDER}/kind-linux-amd64 create cluster --config kind.yml --image kindest/node:"${K8S_VERSION}" --name kind --wait 300s -v=1
}

create_test_kubeconfig() {
  echo "Creating another kubeconfig"
  "${KIND_FOLDER}"/kind-linux-amd64 export kubeconfig --kubeconfig "${TEST_KUBECONFIG}"
  kubectl config rename-context kind-kind "${TEST_KUBECONTEXT}" --kubeconfig "${TEST_KUBECONFIG}"
}

destroy-kind() {
  echo "Destroying kind server"
  "${KIND_FOLDER}"/kind-linux-amd64 delete cluster
}

setup-prometheus() {
  echo "Setting up prometheus instance"
  $OCI_BIN run --rm -d --name prometheus --network=host docker.io/prom/prometheus:latest
  sleep 10
}

check_ns() {
  echo "Checking the number of namespaces labeled with \"${1}\" is \"${2}\""
  if [[ $(kubectl get ns -l "${1}" -o name | wc -l) != "${2}" ]]; then
    echo "Number of namespaces labeled with ${1} less than expected"
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

check_destroyed_pods() {
  echo "Checking pods have been destroyed in namespace ${1}"
  if [[ $(kubectl get pod -n "${1}" -l "${2}" -o name | wc -l) != 0 ]]; then
    echo "Pods in namespace ${1} not destroyed"
    return 1
  fi
}

check_running_pods() {
  running_pods=$(kubectl get pod -A -l ${1} --field-selector=status.phase==Running --no-headers | wc -l)
  if [[ "${running_pods}" != "${2}" ]]; then
    echo "Running pods in cluster labeled with ${1} different from expected: Expected=${2}, observed=${running_pods}"
    return 1
  fi
}

check_running_pods_in_ns() {
    running_pods=$(kubectl get pod -n "${1}" -l kube-burner-job=namespaced | grep -c Running)
    if [[ "${running_pods}" != "${2}" ]]; then
      echo "Running pods in namespace $1 different from expected. Expected=${2}, observed=${running_pods}"
      return 1
    fi
}

check_file_list() {
  for f in "${@}"; do
    if [[ ! -f ${f} ]]; then
      echo "File ${f} not found"
      echo "Content of $(dirname ${f}):"
      ls -l "$(dirname ${f})"
      return 1
    fi
    if [[ $(jq .[0].metricName ${f}) == "" ]]; then
      echo "Incorrect format in ${f}"
      cat "${f}"
      return 1
    fi
  done
  return 0
}

print_events() {
  kubectl get events --sort-by='.lastTimestamp' -A
}

check_metric_value() {
  sleep 3s # There's some delay on the documents to show up in OpenSearch
  for metric in "${@}"; do
    endpoint="${ES_SERVER}/${ES_INDEX}/_search?q=uuid.keyword:${UUID}+AND+metricName.keyword:${metric}"
    RESULT=$(curl -sS ${endpoint} | jq '.hits.total.value // error')
    RETURN_CODE=$?
    if [ "${RETURN_CODE}" -ne 0 ]; then
      echo "Return code: ${RETURN_CODE}"
      return 1
    elif [ "${RESULT}" == 0 ]; then
      echo "${metric} not found in ${endpoint}"
      return 1
    else
      return 0
    fi
  done
}

run_cmd(){
  echo "$@"
  ${@}
}
