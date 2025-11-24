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
  export LOCAL_INDEXING=true
  cp kube-burner.yml /tmp/kube-burner.yml
  run_cmd ${KUBE_BURNER} init -c /tmp/kube-burner.yml --uuid=${UUID} --log-level=debug
  verify_object_count TestCR 5 "" kube-burner.io/uuid=${UUID}
  check_file_exists "kube-burner-${UUID}.log"
  kubectl delete -f objectTemplates/crd.yml
  JOB_NAME=namespaced-${BATS_TEST_NUMBER}
  verify_object_count namespace 5 "" kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID}
  verify_object_count pod 10 "" kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID} status.phase==Running
  verify_object_count pod 5 default kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID} status.phase==Running
  ${KUBE_BURNER} destroy --uuid ${UUID}
  kubectl delete pod -l kube-burner.io/uuid=${UUID} -n default
  verify_object_count namespace 0 "" kube-burner.io/uuid=${UUID}
  verify_object_count pod 0 default kube-burner.io/uuid=${UUID}
  check_file_list ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/jobLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/jobLatencyQuantilesMeasurement-${JOB_NAME}.json
}

# bats test_tags=subsystem:core
@test "kube-burner init: churn-mode=objects, local-indexing=true; os-indexing=true" {
  export ES_INDEXING=true
  export ALERTING=true
  export CHURN_CYCLES=2
  export JOBGC=true
  export CHURN_MODE=object
  export SVC_LATENCY=true
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid=${UUID} --log-level=debug
  JOB_NAME=namespaced-${BATS_TEST_NUMBER}
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS vmiLatencyMeasurement vmiLatencyQuantilesMeasurement jobLatencyMeasurement jobLatencyQuantilesMeasurement svcLatencyMeasurement svcLatencyQuantilesMeasurement alert
  verify_object_count namespace 0 "" kube-burner.io/uuid=${UUID}
  verify_object_count pod 0 "" kube-burner.io/uuid=${UUID}
  check_file_list ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/svcLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-${JOB_NAME}.json
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
  JOB_NAME=namespaced-${BATS_TEST_NUMBER}
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-${JOB_NAME}.json
}

# bats test_tags=subsystem:indexing
@test "kube-burner index: local-indexing=true; os-indexing=true; tarball=true" {
  sleep 1m 
  run_cmd ${KUBE_BURNER} index --uuid=${UUID} -u http://localhost:9090 -m "metrics-profile.yaml,metrics-profile.yaml" --tarball-name=metrics.tgz --start="$(date -d "-1 minutes" +%s)" --metrics-directory=${METRICS_FOLDER}
  check_file_list ${METRICS_FOLDER}/top2PrometheusCPU.json ${METRICS_FOLDER}/top2PrometheusCPU-start.json ${METRICS_FOLDER}/prometheusRSS.json
  run_cmd ${KUBE_BURNER} import --tarball=metrics.tgz --es-server=${ES_SERVER} --es-index=${ES_INDEX} --metrics-directory=${METRICS_FOLDER}
  rm -rf ${METRICS_FOLDER:?}/*
  run_cmd ${KUBE_BURNER} index --uuid=${UUID} -e metrics-endpoints.yaml --es-server=${ES_SERVER} --es-index=${ES_INDEX} --metrics-directory=${METRICS_FOLDER}
  check_file_list ${METRICS_FOLDER}/top2PrometheusCPU.json ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/prometheusRSS.json
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
  check_custom_status_path kube-burner.io/uuid=${UUID} "{.items[*].status.conditions[].type}" Available
  ${KUBE_BURNER} destroy --uuid ${UUID}
}

# bats test_tags=subsystem:job-type-delete
@test "kube-burner init: delete=true" {
  run_cmd ${KUBE_BURNER} init -c kube-burner-delete.yml --uuid ${UUID} --log-level=debug
  verify_object_count namespace 0 "" kube-burner.io/uuid=${UUID}
}

# bats test_tags=subsystem:job-type-read
@test "kube-burner init: read" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-read.yml --uuid ${UUID} --log-level=debug
}

# bats test_tags=subsystem:health-check
@test "kube-burner cluster health check" {
  run_cmd ${KUBE_BURNER} health-check
}

# bats test_tags=subsystem:alerting
@test "kube-burner check-alerts" {
  run_cmd ${KUBE_BURNER} check-alerts -a alerts.yml -u http://localhost:9090 --metrics-directory=alerts
  check_file_list alerts/alert.json
}

# bats test_tags=subsystem:job-type-patch
@test "kube-burner init: sequential patch" {
  export NAMESPACE="sequential-patch"
  export LABEL_KEY="sequential.patch.test"
  export LABEL_VALUE_START="start"
  export LABEL_VALUE_END="end"
  export REPLICAS=50

  # Create a failing deployment to test that kube-burner is not waiting on it
  run_cmd kubectl create deployment failing-up --image=quay.io/cloud-bulldozer/sampleapp:nonexistent --replicas=1

  run_cmd ${KUBE_BURNER} init -c  kube-burner-sequential-patch.yml --uuid=${UUID} --log-level=debug
  verify_object_count deployment ${REPLICAS} ${NAMESPACE} ${LABEL_KEY}=${LABEL_VALUE_END}
  run_cmd kubectl delete ns ${NAMESPACE}
  run_cmd kubectl delete deployment failing-up
}

# bats test_tags=subsystem:job-type-kubevirt
@test "kube-burner init: jobType kubevirt" {
  run_cmd ${KUBE_BURNER} init -c kube-burner-virt-operations.yml --uuid=${UUID} --log-level=debug
}

# bats test_tags=subsystem:core
@test "kube-burner init: user data file" {
  export NAMESPACE="userdata"
  export deploymentLabelFromEnv="from-env"
  export deploymentLabelFromFileOverride="from-env"
  export REPLICAS=5

  run_cmd ${KUBE_BURNER} init -c kube-burner-userdata.yml --user-data=objectTemplates/userdata-test.yml --uuid=${UUID} --log-level=debug
  # Verify that both labels were set
  verify_object_count deployment 0 ${NAMESPACE} kube-burner.io/from-file=unset
  verify_object_count deployment 0 ${NAMESPACE} kube-burner.io/from-env=unset
  # Verify that the from-file label was set from the user-data file
  verify_object_count deployment ${REPLICAS} ${NAMESPACE} kube-burner.io/from-file=from-file
  # Verify that the from-env label was set from the environment variable
  verify_object_count deployment ${REPLICAS} ${NAMESPACE} kube-burner.io/from-env=from-env
  # Verify that the default value is used when the variable is not set
  verify_object_count deployment ${REPLICAS} ${NAMESPACE} kube-burner.io/unset=unset
  # Verify that the from-file-override label was set from the input file
  verify_object_count deployment ${REPLICAS} ${NAMESPACE} kube-burner.io/from-file-override=from-file
  kubectl delete ns ${NAMESPACE}
}

# bats test_tags=subsystem:measurements
@test "kube-burner init: datavolume latency" {
  if [[ -z "$VOLUME_SNAPSHOT_CLASS_NAME" ]]; then
    echo "VOLUME_SNAPSHOT_CLASS_NAME must be set when using USE_EXISTING_CLUSTER"
    return 1
  fi
  export STORAGE_CLASS_NAME=${STORAGE_CLASS_NAME:-$STORAGE_CLASS_WITH_SNAPSHOT_NAME}
  if [[ -z "$STORAGE_CLASS_NAME" ]]; then
    echo "STORAGE_CLASS_NAME must be set when using USE_EXISTING_CLUSTER"
    return 1
  fi

  run_cmd ${KUBE_BURNER} init -c kube-burner-dv.yml --uuid=${UUID} --log-level=debug

  # Verify metrics for PVC and DV were collected
  local jobs=("create-vm" "create-base-image-dv")
  for job in "${jobs[@]}"; do
    check_metric_recorded ${job} dvLatency dvReadyLatency
    check_metric_recorded ${job} pvcLatency bindingLatency
    check_quantile_recorded ${job} dvLatency Ready
    check_quantile_recorded ${job} pvcLatency Bound
  done

  # Verify that metrics for VolumeSnapshot was collected
  check_metric_recorded create-snapshot volumeSnapshotLatency vsReadyLatency
  check_quantile_recorded create-snapshot volumeSnapshotLatency Ready
}

# bats test_tags=subsystem:measurements
@test "Verify measurements configuration" {
  export LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-measurements.yml --uuid=${UUID} --log-level=debug

  # Verify all jobs have podLatency
  local jobs_with_pod=("precedence-measurements" "merge-measurements")
  for job in "${jobs_with_pod[@]}"; do
    check_metric_recorded ${job} podLatency podReadyLatency
    check_quantile_recorded ${job} podLatency Ready
  done

  # Verify only merge-measurements adds serviceLatency
  check_metric_recorded merge-measurements svcLatency ready
  check_quantile_recorded merge-measurements svcLatency Ready
  check_metrics_not_created_for_job precedence-measurements svcLatency

  # Verify all expected metric files were created
  check_file_list ${METRICS_FOLDER}/podLatencyMeasurement-precedence-measurements.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-precedence-measurements.json ${METRICS_FOLDER}/podLatencyMeasurement-merge-measurements.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-merge-measurements.json ${METRICS_FOLDER}/svcLatencyMeasurement-merge-measurements.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-merge-measurements.json
}