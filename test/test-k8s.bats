#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2030,SC2031,SC2164

load helpers.bash

setup_file() {
  cd k8s
  export BATS_TEST_TIMEOUT=600
  export JOB_ITERATIONS=4
  export QPS=3
  export BURST=3
  export GC=true
  export CHURN=false
  export CHURN_CYCLES=100
  export TEST_KUBECONFIG; TEST_KUBECONFIG=$(mktemp -d)/kubeconfig
  export TEST_KUBECONTEXT=test-context
  setup-kind
  create_test_kubeconfig
  setup-prometheus
}

setup() {
  export UUID; UUID=$(uuidgen)
  export ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com"
  export ES_INDEX="kube-burner"
  export METRICS_FOLDER="metrics-${UUID}"
  export ES_INDEXING=""
  export LOCAL_INDEXING=""
}

teardown() {
  kubectl delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

teardown_file() {
  destroy-kind
  $OCI_BIN rm -f prometheus
}


@test "kube-burner init: churn=true" {
  export CHURN=true
  export CHURN_CYCLES=2
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: gc=false" {
  export GC=false
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}" 5
  check_running_pods kube-burner-job=namespaced,kube-burner-uuid="${UUID}" 10
  check_running_pods_in_ns default 5
  kube-burner destroy --uuid "${UUID}"
  kubectl delete pod -l kube-burner-uuid=${UUID} -n default
  check_destroyed_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: local-indexing=true; pod-latency-metrics-indexing=true" {
  export LOCAL_INDEXING=true
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_file_list ${METRICS_FOLDER}/jobSummary-namespaced.json ${METRICS_FOLDER}/podLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: os-indexing=true; alerting=true"  {
  export ES_INDEXING=true
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml -a alert-profile.yaml
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement alert
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: os-indexing=true; local-indexing=true; metrics-endpoint=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -e metrics-endpoints.yaml
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement svcLatencyMeasurement svcLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary-namespaced.json ${METRICS_FOLDER}/podLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner index: local-indexing=true; tarball=true" {
  run_cmd kube-burner index --uuid="${UUID}" -u http://localhost:9090 -m metrics-profile.yaml --tarball-name=metrics.tgz
  check_file_list collected-metrics/top2PrometheusCPU.json collected-metrics/prometheusRSS.json
  run_cmd kube-burner import --tarball=metrics.tgz --es-server=${ES_SERVER} --es-index=${ES_INDEX}
}

@test "kube-burner index: metrics-endpoint=true; os-indexing=true" {
  run_cmd kube-burner index --uuid="${UUID}" -e metrics-endpoints.yaml --es-server=${ES_SERVER} --es-index=${ES_INDEX}
  check_metric_value top2PrometheusCPU prometheusRSS
}

@test "kube-burner init: crd" {
  kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/network-attachment-definition-client/master/artifacts/networks-crd.yaml
  sleep 5s
  run_cmd kube-burner init -c kube-burner-crd.yml --uuid="${UUID}"
  kubectl delete -f objectTemplates/storageclass.yml
}

@test "kube-burner init: delete=true; os-indexing=true; local-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd kube-burner init -c kube-burner-delete.yml --uuid "${UUID}" --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary-delete-job.json ${METRICS_FOLDER}/podLatencyMeasurement-delete-job.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-delete-job.json ${METRICS_FOLDER}/prometheusBuildInfo.json
}

@test "kube-burner init: read; os-indexing=true; local-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd kube-burner init -c kube-burner-read.yml --uuid "${UUID}" --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary-read-job.json ${METRICS_FOLDER}/prometheusBuildInfo.json
}

@test "kube-burner init: kubeconfig" {
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug --kubeconfig="${TEST_KUBECONFIG}"
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: kubeconfig kube-context" {
  run_cmd kubectl --kubeconfig "${TEST_KUBECONFIG}" config unset current-context
  run_cmd kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug --kubeconfig="${TEST_KUBECONFIG}" --kube-context="${TEST_KUBECONTEXT}"
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner cluster health check" {
  run_cmd kube-burner health-check
}
