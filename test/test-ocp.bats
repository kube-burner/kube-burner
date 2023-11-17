#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2164

load helpers.bash

setup_file() {
  cd ocp
  export BATS_TEST_TIMEOUT=600
  export ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com"
  export ES_INDEX="kube-burner-ocp"
  trap print_events ERR
  setup-prometheus
}

setup() {
  export UUID; UUID=$(uuidgen)
  export COMMON_FLAGS="--es-server=${ES_SERVER} --es-index=${ES_INDEX} --alerting=true --uuid=${UUID} --qps=5 --burst=5"
}

teardown() {
  echo "Last bats run command: ${BATS_RUN_COMMAND} from $(pwd)"
  oc delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

teardown_file() {
  podman rm -f prometheus
}

@test "node-density with indexing" {
  run kube-burner ocp node-density --pods-per-node=75 --pod-ready-threshold=10s ${COMMON_FLAGS}
  [ "$status" -eq 0 ]
  run check_metric_value etcdVersion clusterMetadata jobSummary podLatencyMeasurement podLatencyQuantilesMeasurement
  [ "$status" -eq 0 ]
}

@test "node-density-heavy with indexing" {
  run kube-burner ocp node-density-heavy --pods-per-node=75 --uuid=abcd --local-indexing --gc-metrics=true
  [ "$status" -eq 0 ]
  run check_file_list collected-metrics-abcd/etcdVersion.json collected-metrics-abcd/clusterMetadata.json collected-metrics-abcd/jobSummary-node-density-heavy.json collected-metrics-abcd/jobSummary-garbage-collection.json collected-metrics-abcd/podLatencyMeasurement-node-density-heavy.json collected-metrics-abcd/podLatencyQuantilesMeasurement-node-density-heavy.json
  [ "$status" -eq 0 ]
}

@test "cluster-density-ms: metrics-endpoint=true; es-indexing=true" {
  run kube-burner ocp cluster-density-ms --iterations=1 --churn=false --metrics-endpoint metrics-endpoints.yaml ${COMMON_FLAGS}
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata jobSummary podLatencyMeasurement podLatencyQuantilesMeasurement
  [ "$status" -eq 0 ]
}

@test "cluster-density-v2: profile-type=both; user-metadata=true; es-indexing=true; churning=true" {
  run kube-burner ocp cluster-density-v2 --iterations=5 --churn-duration=1m --churn-delay=5s --profile-type=both ${COMMON_FLAGS} --user-metadata=user-metadata.yml
  [ "$status" -eq 0 ]
  run check_metric_value cpu-kubelet clusterMetadata jobSummary podLatencyMeasurement podLatencyQuantilesMeasurement etcdVersion
  [ "$status" -eq 0 ]
}

@test "cluster-density-v2 with gvr churn deletion strategy" {
  run kube-burner ocp cluster-density-v2 --iterations=2 --churn=true --churn-duration=1m --churn-delay=10s --churn-deletion-strategy=gvr ${COMMON_FLAGS}
  [ "$status" -eq 0 ]
  run check_metric_value etcdVersion clusterMetadata jobSummary podLatencyMeasurement podLatencyQuantilesMeasurement
  [ "$status" -eq 0 ]
}

@test "cluster-density-v2: indexing=false; churning=false" {
  run kube-burner ocp cluster-density-v2 --iterations=2 --churn=false --uuid=${UUID}
  [ "$status" -eq 0 ]
}

@test "node-density-cni: gc=false; alerting=false" {
  # Disable gc and avoid metric indexing
  run kube-burner ocp node-density-cni --pods-per-node=75 --gc=false --uuid=${UUID} --alerting=false
  oc delete ns -l kube-burner-uuid=${UUID}
  trap - ERR
}

@test "cluster-density-v2 timeout check" {
  run kube-burner ocp cluster-density-v2 --iterations=1 --churn-duration=5m --timeout=1s
  [ "$status" -eq 2 ]
}

@test "index: local-indexing=true" {
  run kube-burner ocp index --uuid="${UUID}" --metrics-profile metrics-profile.yaml
  [ "$status" -eq 0 ]
}

@test "index: metrics-endpoints=true; es-indexing=true" {
  run kube-burner ocp index --uuid="${UUID}" --metrics-endpoint metrics-endpoints.yaml --metrics-profile metrics-profile.yaml --es-server=https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com:443 --es-index=ripsaw-kube-burner
  [ "$status" -eq 0 ]
}

@test "networkpolicy-multitenant" {
  run kube-burner ocp networkpolicy-multitenant --iterations 5  ${COMMON_FLAGS}
  [ "$status" -eq 0 ]
}

@test "pvc-density" {
  # Since 'aws' is the chosen storage provisioner, this will only execute successfully if the ocp environment is aws
  run kube-burner ocp pvc-density --iterations=2 --provisioner=aws ${COMMON_FLAGS}
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata jobSummary podLatencyMeasurement podLatencyQuantilesMeasurement
  [ "$status" -eq 0 ]
}
