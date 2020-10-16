#!/bin/bash -e

set -e
rc=0
uuid=$(uuidgen)

check_ns() {
  if [[ $(kubectl get ns -l ${1} -o name | wc -l) != ${2} ]]; then
    echo "Number of namespaces labeled with ${1} less than expected"
    rc=1
  fi
}

check_destroyed_ns() {
  if [[ $(kubectl get ns -l ${1} -o name | wc -l) != 0 ]]; then
    echo "Namespaces labeled with ${1} not destroyed"
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
    echo "Running pods in namespaces labeled with ${1} different from expected"
    rc=1
  fi
}

kube-burner init -c kube-burner.yml --uuid ${uuid} --log-level=debug
check_ns kube-burner-job=not-namespaced,kube-burner-uuid=${uuid} 1
check_ns kube-burner-job=namespaced,kube-burner-uuid=${uuid} 5
check_running_pods kube-burner-job=namespaced,kube-burner-uuid=${uuid} 5 
kube-burner destroy -c kube-burner.yml --uuid ${uuid}
check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid=${uuid}
check_destroyed_ns kube-burner-job=namespaced,kube-burner-uuid=${uuid}
exit ${rc}


