#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2030,SC2031,SC2164

load helpers.bash

# bats test_tags=subsystem:core
@test "kube-burner init: gc=false; preload=true; churn-mode=namespaces; local-indexing=true; crd=true" {
  export CHURN_CYCLES=2
  export GC=false
  export PRELOAD_IMAGES=true
  export CRD=true
  cp kube-burner.yml /tmp/kube-burner.yml
  run_cmd ${KUBE_BURNER} init -c /tmp/kube-burner.yml --uuid=${UUID} --log-level=debug
  verify_object_count TestCR 5 cr-crd kube-burner-uuid=${UUID}
  check_file_exists "kube-burner-${UUID}.log"
  kubectl delete -f objectTemplates/crd.yml
  verify_object_count namespace 5 "" kube-burner-job=namespaced,kube-burner-uuid=${UUID}
  verify_object_count pod 10 "" kube-burner-job=namespaced,kube-burner-uuid=${UUID} status.phase==Running
  verify_object_count pod 5 default kube-burner-job=namespaced,kube-burner-uuid=${UUID} status.phase==Running
  ${KUBE_BURNER} destroy --uuid ${UUID}
  kubectl delete pod -l kube-burner-uuid=${UUID} -n default
  verify_object_count namespace 0 "" kube-burner-uuid=${UUID}
  verify_object_count pod 0 default kube-burner-uuid=${UUID}
  check_file_list ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/jobLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/jobLatencyQuantilesMeasurement-namespaced.json
}

# bats test_tags=subsystem:core
@test "kube-burner init: churn-mode=objects, local-indexing=true; os-indexing=true" {
  export ES_INDEXING=true
  export ALERTING=true
  export CHURN_CYCLES=2
  export JOBGC=true
  export PRELOAD_IMAGES=true
  export CHURN_MODE=objects
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid=${UUID} --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS vmiLatencyMeasurement vmiLatencyQuantilesMeasurement jobLatencyMeasurement jobLatencyQuantilesMeasurement alert
  verify_object_count namespace 0 "" kube-burner-uuid=${UUID}
  verify_object_count pod 0 "" kube-burner-uuid=${UUID}
  check_file_list ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
}

# bats test_tags=subsystem:indexing
@test "kube-burner init: metrics aggregation" {
  export STORAGE_CLASS_NAME
  STORAGE_CLASS_NAME=$(get_default_storage_class)
  run_cmd ${KUBE_BURNER} init -c kube-burner-metrics-aggregate.yml --uuid=${UUID} --log-level=debug

  local aggr_job="create-vms"
  local metric="vmiLatency"
  check_metric_recorded ${aggr_job} ${metric} vmReadyLatency
  check_quantile_recorded ${aggr_job} ${metric} VMReady

  local skipped_jobs=("start-vm" "wait-running")
  for job in "${skipped_jobs[@]}"; do
    check_metrics_not_created_for_job ${job} ${metric}
    check_metrics_not_created_for_job ${job} ${metric}
  done
}

# bats test_tags=subsystem:indexing
@test "kube-burner init: os-indexing=true; local-indexing=true; metrics-endpoint=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true TIMESERIES_INDEXER=local-indexing
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid=${UUID} --log-level=debug -e metrics-endpoints.yaml
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyMeasurement-namespaced.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-namespaced.json
}

# bats test_tags=subsystem:indexing,ci:parallel
@test "kube-burner index: local-indexing=true; tarball=true" {
  run_cmd ${KUBE_BURNER} index --uuid=${UUID} -u http://localhost:9090 -m "metrics-profile.yaml,metrics-profile.yaml" --tarball-name=metrics.tgz --start="$(date -d "-2 minutes" +%s)"
  check_file_list collected-metrics/top2PrometheusCPU.json collected-metrics/top2PrometheusCPU-start.json collected-metrics/prometheusRSS.json
  run_cmd ${KUBE_BURNER} import --tarball=metrics.tgz --es-server=${ES_SERVER} --es-index=${ES_INDEX}
}

# bats test_tags=subsystem:indexing,ci:parallel
@test "kube-burner index: metrics-endpoint=true; os-indexing=true" {
  run_cmd ${KUBE_BURNER} index --uuid=${UUID} -e metrics-endpoints.yaml --es-server=${ES_SERVER} --es-index=${ES_INDEX}
  check_file_list collected-metrics/top2PrometheusCPU.json collected-metrics/prometheusRSS.json collected-metrics/prometheusRSS.json
}

# bats test_tags=subsystem:custom-kubeconfig
@test "kube-burner init: kubeconfig" {
  run_cmd kubectl --kubeconfig "${TEST_KUBECONFIG}" config unset current-context
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid=${UUID} --log-level=debug --kubeconfig="${TEST_KUBECONFIG}" --kube-context="${TEST_KUBECONTEXT}"
}

# bats test_tags=subsystem:waiters
@test "kube-burner init: waitOptions for Deployment" {
  export GC=false
  export WAIT_FOR_CONDITION="True"
  export WAIT_CUSTOM_STATUS_PATH='(.conditions.[] | select(.type == "Available")).status'
  run_cmd ${KUBE_BURNER} init -c  kube-burner.yml --uuid=${UUID} --log-level=debug
  check_custom_status_path kube-burner-uuid=${UUID} "{.items[*].status.conditions[].type}" Available
  ${KUBE_BURNER} destroy --uuid ${UUID}
}