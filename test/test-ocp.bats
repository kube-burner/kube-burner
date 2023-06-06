#!/usr/bin/env bats
# vi: ft=bash

setup_file() {
  export BATS_TEST_TIMEOUT=600
  export ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com"
  export ES_INDEX="kube-burner-ocp"
  trap print_events ERR
}

setup() {
  load helpers.bash
  export UUID; UUID=$(uuidgen)
  export COMMON_FLAGS=" --es-server=${ES_SERVER} --es-index=${ES_INDEX} --alerting=true --uuid=${UUID} --qps=5 --burst=5"
}

teardown() {
  oc delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

@test "node-density with indexing" {
  run kube-burner ocp node-density --pods-per-node=75 --pod-ready-threshold=10s --container-image=gcr.io/google_containers/pause:3.0 "${COMMON_FLAGS}"
  run check_metric_value etcdVersion
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata
  [ "$status" -eq 0 ]
  run check_metric_value jobSummary
  [ "$status" -eq 0 ]
}

@test "node-density-heavy with indexing" {
  run kube-burner ocp node-density-heavy --pods-per-node=75 "${COMMON_FLAGS}"
  run check_metric_value etcdVersion
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata
  [ "$status" -eq 0 ]
  run check_metric_value jobSummary
  [ "$status" -eq 0 ]
}

@test "cluster-density with user metadata and indexing" {
  run kube-burner ocp cluster-density --iterations=2 --churn-duration=2m "${COMMON_FLAGS}" --user-metadata=user-metadata.yml
  run check_metric_value etcdVersion
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata
  [ "$status" -eq 0 ]
  run check_metric_value jobSummary
  [ "$status" -eq 0 ]
}

@test "cluster-density" {
  run kube-burner ocp cluster-density --iterations=2 --churn=false --uuid="${UUID}"
}

@test "cluster-density-ms for multiple endpoints case with indexing" {
  run kube-burner ocp cluster-density-ms --iterations=1 --churn-duration=2m --metrics-endpoint metrics-endpoints.yaml "${COMMON_FLAGS}"
  run check_metric_value etcdVersion
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata
  [ "$status" -eq 0 ]
  run check_metric_value jobSummary
  [ "$status" -eq 0 ]
}

@test "cluster-density-v2 with indexing" {
  run kube-burner ocp cluster-density-v2 --iterations=2 --churn-duration=2m "${COMMON_FLAGS}"
  run check_metric_value etcdVersion
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata
  [ "$status" -eq 0 ]
  run check_metric_value jobSummary
  [ "$status" -eq 0 ]
}

@test "node-density-cni with gc=false and no indexing" {
  # Disable gc and avoid metric indexing
  run kube-burner ocp node-density-cni --pods-per-node=75 --gc=false --uuid="${UUID}" --alerting=false
  oc delete ns -l kube-burner-uuid="${UUID}"
  trap - ERR
}

@test "cluster-density timeout case with indexing" {
  run  kube-burner ocp cluster-density --iterations=1 --churn-duration=5m "${COMMON_FLAGS}" --timeout=1s
  [ "$status" -eq 2 ]
}
