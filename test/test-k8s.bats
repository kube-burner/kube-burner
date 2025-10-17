#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2030,SC2031,SC2164

load helpers.bash

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

@test "kube-burner init: os-indexing=true; local-indexing=true; vm-latency-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true ALERTING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-virt.yml --uuid="${UUID}" --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS vmiLatencyMeasurement vmiLatencyQuantilesMeasurement alert
  check_file_list ${METRICS_FOLDER}/jobSummary.json  ${METRICS_FOLDER}/vmiLatencyMeasurement-kubevirt-density.json ${METRICS_FOLDER}/vmiLatencyQuantilesMeasurement-kubevirt-density.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
}

@test "kube-burner init: crd" {
  kubectl apply -f objectTemplates/burnerTest-crd.yml
  sleep 5
  run_cmd ${KUBE_BURNER} init -c kube-burner-crd.yml --uuid="${UUID}"
  kubectl delete -f objectTemplates/storageclass.yml
  kubectl delete -f objectTemplates/burnerTest-crd.yml
}

@test "kube-burner init: delete=true; os-indexing=true; local-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-delete.yml --uuid "${UUID}" --log-level=debug
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-delete-job.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-delete-job.json ${METRICS_FOLDER}/prometheusBuildInfo.json
}

@test "kube-burner init: read; os-indexing=true; local-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-read.yml --uuid "${UUID}" --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/prometheusBuildInfo.json
}

@test "kube-burner cluster health check" {
  run_cmd ${KUBE_BURNER} health-check
}

@test "kube-burner check-alerts" {
  run_cmd ${KUBE_BURNER} check-alerts -a alerts.yml -u http://localhost:9090 --metrics-directory=alerts
  check_file_list alerts/alert.json
}

@test "kube-burner init: sequential patch" {
  export NAMESPACE="sequential-patch"
  export LABEL_KEY="sequential.patch.test"
  export LABEL_VALUE_START="start"
  export LABEL_VALUE_END="end"
  export REPLICAS=50

  # Create a failing deployment to test that kube-burner is not waiting on it
  run_cmd kubectl create deployment failing-up --image=quay.io/cloud-bulldozer/sampleapp:nonexistent --replicas=1

  run_cmd ${KUBE_BURNER} init -c  kube-burner-sequential-patch.yml --uuid="${UUID}" --log-level=debug
  check_deployment_count ${NAMESPACE} ${LABEL_KEY} ${LABEL_VALUE_END} ${REPLICAS}
  run_cmd kubectl delete ns ${NAMESPACE}
  run_cmd kubectl delete deployment failing-up
}

@test "kube-burner init: jobType kubevirt" {
  run_cmd ${KUBE_BURNER} init -c  kube-burner-virt-operations.yml --uuid="${UUID}" --log-level=debug
}

@test "kube-burner init: user data file" {
  export NAMESPACE="userdata"
  export deploymentLabelFromEnv="from-env"
  export deploymentLabelFromFileOverride="from-env"
  export REPLICAS=5

  run_cmd ${KUBE_BURNER} init -c kube-burner-userdata.yml --user-data=objectTemplates/userdata-test.yml --uuid="${UUID}" --log-level=debug
  # Verify that both labels were set
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-file" "unset" 0
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-env" "unset" 0
  # Verify that the from-file label was set from the user-data file
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-file" "from-file" ${REPLICAS}
  # Verify that the from-env label was set from the environment variable
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-env" "from-env" ${REPLICAS}
  # Verify that the default value is used when the variable is not set
  check_deployment_count ${NAMESPACE} "kube-burner.io/unset" "unset" ${REPLICAS}
  # Verify that the from-file-override label was set from the input file
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-file-override" "from-file" ${REPLICAS}
  kubectl delete ns ${NAMESPACE}
}

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

  run_cmd ${KUBE_BURNER} init -c kube-burner-dv.yml --uuid="${UUID}" --log-level=debug

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

@test "kube-burner init: metrics aggregation" {
  export STORAGE_CLASS_NAME
  STORAGE_CLASS_NAME=$(get_default_storage_class)
  run_cmd ${KUBE_BURNER} init -c kube-burner-metrics-aggregate.yml --uuid="${UUID}" --log-level=debug

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

@test "kube-burner init: create CRD and CR together" {
  run_cmd ${KUBE_BURNER} init -c kube-burner-cr-crd.yml --uuid="${UUID}"
  check_running_custom_resources_in_ns testcr cr-crd 5
}

@test "Verify measurements configuration" {
  export LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-measurements.yml --uuid="${UUID}" --log-level=debug

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
