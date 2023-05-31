#!/usr/bin/env bats
# vi: ft=bash

setup_file() {
  export BATS_TEST_TIMEOUT=600
  export TEMP_FOLDER; TEMP_FOLDER=$(mktemp -d)
  export UUID; UUID=$(uuidgen)
  export JOB_ITERATIONS=5
  export QPS=2
  export BURST=2
  export ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com/"
  export ES_INDEX="kube-burner-ocp"
  export COMMON_FLAGS=" --es-server ${ES_SERVER} --es-index ${ES_INDEX} --alerting=true --uuid=${UUID} --qps=5 --burst=5"
  load helpers.bash
  oc login -u "${OPENSHIFT_USER}" -p "${OPENSHIFT_PASSWORD}" "${OPENSHIFT_SERVER}" --insecure-skip-tls-verify=true
  trap print_events ERR
}

setup() {
  load helpers.bash
}

teardown() {
  oc delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

@test "Running node-density wrapper" {
  run kube-burner ocp node-density --pods-per-node=75 --pod-ready-threshold=10s --container-image=gcr.io/google_containers/pause:3.0 "${COMMON_FLAGS}"
  hits=$(curl "${ES_SERVER}/${ES_INDEX}/_search?q=uuid.keyword:${UUID}+AND+metricName.keyword:etcdVersion" | jq .hits.total.value)
  if [[ "${hits}" == 0 ]]; then
    echo "Couldn't find the metric etcdVersion indexed in ES"
    exit 1
  fi
}

@test "Running node-density-heavy wrapper" {
  run kube-burner ocp node-density-heavy --pods-per-node=75 "${COMMON_FLAGS}" --qps=5 --burst=5
}

@test "Running cluster-density wrapper and user metadata" {
  run kube-burner ocp cluster-density --iterations=2 --churn-duration=2m "${COMMON_FLAGS}" --user-metadata=user-metadata.yml
}

@test "Running cluster-density wrapper" {
  run kube-burner ocp cluster-density --iterations=2 --churn=false --uuid="${UUID}"
}

@test "Running cluster-density-ms wrapper for multiple endpoints case" {
  run kube-burner ocp cluster-density-ms --iterations=1 --churn-duration=2m --metrics-endpoint metrics-endpoints.yaml "${COMMON_FLAGS}"
}

@test "Running cluster-density-v2 wrapper" {
  run kube-burner ocp cluster-density-v2 --iterations=2 --churn-duration=2m "${COMMON_FLAGS}"
}

@test "Running node-density-cni wrapper with gc=false" {
  # Disable gc and avoid metric indexing
  run kube-burner ocp node-density-cni --pods-per-node=75 --gc=false --uuid="${UUID}" --alerting=false
  oc delete ns -l kube-burner-uuid="${UUID}"
  trap - ERR
}

@test "Running cluster-density timeout case" {
  run timeout 600 kube-burner ocp cluster-density --iterations=1 --churn-duration=5m "${COMMON_FLAGS}" --timeout=1s
  [ "$status" -eq 2 ]
}
