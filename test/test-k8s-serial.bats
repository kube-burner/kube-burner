#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2030,SC2031,SC2164

load helpers.bash
export BATS_NO_PARALLELIZE_WITHIN_FILE=true

setup() {
  export UUID; UUID=$(uuidgen)
  export METRICS_FOLDER="metrics-${UUID}"
  export ES_INDEXING=""
  export LOCAL_INDEXING=""
  export ALERTING=""
  export TIMESERIES_INDEXER=""
}

teardown() {
  kubectl delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

@test "kube-burner init: churn=true; absolute-path=true; job-gc=true" {
  export CHURN=true
  export CHURN_CYCLES=2
  export GC=false
  export JOBGC=true
  cp kube-burner.yml /tmp/kube-burner.yml
  run_cmd ${KUBE_BURNER} init -c /tmp/kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: gc=false" {
  export GC=false
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}" 5
  check_running_pods kube-burner-job=namespaced,kube-burner-uuid="${UUID}" 10
  check_running_pods_in_ns default 5
  ${KUBE_BURNER} destroy --uuid "${UUID}"
  kubectl delete pod -l kube-burner-uuid=${UUID} -n default
  check_destroyed_ns kube-burner-job=namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: local-indexing=true; pod-latency-metrics-indexing=true" {
  export LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: os-indexing=true; local-indexing=true; alerting=true"  {
  export ES_INDEXING=true LOCAL_INDEXING=true ALERTING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement jobLatencyMeasurement jobLatencyQuantilesMeasurement alert
  check_file_list ${METRICS_FOLDER}/jobSummary.json  ${METRICS_FOLDER}/podLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: os-indexing=true; local-indexing=true; metrics-endpoint=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true TIMESERIES_INDEXER=local-indexing
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -e metrics-endpoints.yaml
  check_file_list ${METRICS_FOLDER}/jobSummary.json  ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner index: local-indexing=true; tarball=true" {
  run_cmd ${KUBE_BURNER} index --uuid="${UUID}" -u http://localhost:9090 -m "metrics-profile.yaml,metrics-profile.yaml" --tarball-name=metrics.tgz --start="$(date -d "-2 minutes" +%s)"
  check_file_list collected-metrics/top2PrometheusCPU.json collected-metrics/top2PrometheusCPU-start.json collected-metrics/prometheusRSS.json
  run_cmd ${KUBE_BURNER} import --tarball=metrics.tgz --es-server=${ES_SERVER} --es-index=${ES_INDEX}
}

@test "kube-burner index: metrics-endpoint=true; os-indexing=true" {
  run_cmd ${KUBE_BURNER} index --uuid="${UUID}" -e metrics-endpoints.yaml --es-server=${ES_SERVER} --es-index=${ES_INDEX}
  check_file_list collected-metrics/top2PrometheusCPU.json collected-metrics/prometheusRSS.json collected-metrics/prometheusRSS.json
}

@test "kube-burner init: kubeconfig" {
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug --kubeconfig="${TEST_KUBECONFIG}"
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: kubeconfig kube-context" {
  run_cmd kubectl --kubeconfig "${TEST_KUBECONFIG}" config unset current-context
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug --kubeconfig="${TEST_KUBECONFIG}" --kube-context="${TEST_KUBECONTEXT}"
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner log file output" {
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_file_exists "kube-burner-${UUID}.log"
}

@test "kube-burner init: waitOptions for Deployment" {
  export GC=false
  export WAIT_FOR_CONDITION="True"
  export WAIT_CUSTOM_STATUS_PATH='(.conditions.[] | select(.type == "Available")).status'
  run_cmd ${KUBE_BURNER} init -c  kube-burner.yml --uuid="${UUID}" --log-level=debug
  check_custom_status_path kube-burner-uuid="${UUID}" "{.items[*].status.conditions[].type}" Available
  ${KUBE_BURNER} destroy --uuid "${UUID}"
}
