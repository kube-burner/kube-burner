#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2164

load helpers.bash

setup_file() {
  cd ocp
  export BATS_TEST_TIMEOUT=5
  export ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com"
  export ES_INDEX="kube-burner-ocp"
  trap print_events ERR
  #setup-prometheus
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

@test "index: local-indexing=true" {
  run kube-burner ocp index --uuid="${UUID}" --metrics-profile metrics-profile.yaml
  [ "$status" -eq 0 ]
  run check_metric_value clusterMetadata jobSummary podLatencyMeasurement podLatencyQuantilesMeasurement
  [ "$status" -eq 0 ]
}
