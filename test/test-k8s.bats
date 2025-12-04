#!/usr/bin/env bats
# vi: ft=bash
# shellcheck disable=SC2086,SC2030,SC2031,SC2164

load helpers.bash

setup_file() {
  cd k8s
  export BATS_TEST_TIMEOUT=1800
  export JOB_ITERATIONS=4
  export QPS=3
  export BURST=3
  export TEST_KUBECONFIG; TEST_KUBECONFIG=$(mktemp -d)/kubeconfig
  export TEST_KUBECONTEXT=test-context
  export ES_SERVER=${PERFSCALE_PROD_ES_SERVER:-"http://localhost:9200"}
  export ES_INDEX="kube-burner"
  export DEPLOY_GRAFANA=${DEPLOY_GRAFANA:-false}
  if [[ "${USE_EXISTING_CLUSTER,,}" != "yes" ]]; then
    setup-kind
  fi
  create_test_kubeconfig
  setup-prometheus
  if [[ -z "$PERFSCALE_PROD_ES_SERVER" ]]; then
    $OCI_BIN rm -f opensearch
    $OCI_BIN network rm -f monitoring
    setup-shared-network
    setup-opensearch
    if [ "$DEPLOY_GRAFANA" = "true" ]; then
      $OCI_BIN rm -f grafana
      setup-grafana
      configure-grafana-datasource
      deploy-grafana-dashboards
    fi
  fi
}

setup() {
  export UUID; UUID=$(uuidgen)
  export METRICS_FOLDER="metrics-${UUID}"
  export ES_INDEXING=""
  export CHURN_MODE=""
  export CHURN_CYCLES=0
  export PRELOAD_IMAGES=false
  export GC=true
  export JOBGC=false
  export LOCAL_INDEXING=""
  export ALERTING=""
  export TIMESERIES_INDEXER=""
  export CRD=""
  export SVC_LATENCY=""
  export JOB_NAME=kube-burner-ci-${BATS_TEST_NUMBER}
}

teardown() {
  kubectl delete ns -l kube-burner.io/uuid=${UUID} --ignore-not-found
}

teardown_file() {
  if [[ "${USE_EXISTING_CLUSTER,,}" != "yes" ]]; then
    destroy-kind
  fi
  $OCI_BIN rm -f prometheus
  if [[ -z "$PERFSCALE_PROD_ES_SERVER" ]]; then
    if [ "$DEPLOY_GRAFANA" = "false" ]; then
      $OCI_BIN rm -f opensearch
      $OCI_BIN network rm -f monitoring
    fi
  fi
}

# bats test_tags=subsystem:preload,subsystem:indexing
@test "kube-burner.yml: preload=true; set-churn-mode=namespaces; set-gc=false" {
  export CRD=true
  cp kube-burner.yml /tmp/kube-burner.yml
  run_cmd ${KUBE_BURNER} init -c /tmp/kube-burner.yml --uuid=${UUID} --set global.gc=false,jobs.0.churnConfig.mode=namespaces,jobs.0.preLoadImages=true,jobs.0.churnConfig.cycles=2 --log-level=debug
  verify_object_count TestCR 5 cr-crd kube-burner.io/uuid=${UUID}
  check_file_exists "kube-burner-${UUID}.log"
  kubectl delete -f objectTemplates/crd.yml
  verify_object_count namespace 5 "" kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID}
  verify_object_count pod 10 "" kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID} status.phase==Running
  verify_object_count pod 5 default kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID} status.phase==Running
  ${KUBE_BURNER} destroy --uuid ${UUID}
  kubectl delete pod -l kube-burner.io/uuid=${UUID} -n default
  verify_object_count namespace 0 "" kube-burner.io/uuid=${UUID}
  verify_object_count pod 0 default kube-burner.io/uuid=${UUID}
}

# bats test_tags=subsystem:indexing
@test "kube-burner.yml: churn-mode=objects, local-indexing=true; os-indexing=true" {
  export CHURN_MODE=objects
  export ES_INDEXING=true LOCAL_INDEXING=true
  export ALERTING=true
  export CHURN_CYCLES=2
  export GC=false JOBGC=true
  export SVC_LATENCY=true
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid=${UUID} --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS vmiLatencyMeasurement vmiLatencyQuantilesMeasurement jobLatencyMeasurement jobLatencyQuantilesMeasurement alert
  verify_object_count namespace 0 "" kube-burner.io/uuid=${UUID}
  verify_object_count pod 0 "" kube-burner.io/uuid=${UUID}
  check_file_list ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/svcLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/svcLatencyQuantilesMeasurement-${JOB_NAME}.json
}

# bats test_tags=subsystem:preload,subsystem:indexing
@test "kube-burner-virt.yml: metrics-endpoints=true; vm-latency-indexing=true; set-preload=true" {
  run_cmd ${KUBE_BURNER} init -c kube-burner-virt.yml --uuid=${UUID} -e metrics-endpoints.yaml --set jobs.0.preLoadImages=true --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS vmiLatencyMeasurement vmiLatencyQuantilesMeasurement alert
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/vmiLatencyMeasurement-${JOB_NAME}.json ${METRICS_FOLDER}/vmiLatencyQuantilesMeasurement-${JOB_NAME}.json
  verify_object_count namespace 0 "" kube-burner.io/job=${JOB_NAME},kube-burner.io/uuid=${UUID}
}

# bats test_tags=subsystem:indexing
@test "kube-burner index: local-indexing=true; tarball=true" {
  sleep 1m # Sleep is required to prevent collecting metrics right after spinning up the cluster, which can lead to missing datapoints
  run_cmd ${KUBE_BURNER} index --uuid=${UUID} -u http://localhost:9090 -m "metrics-profile.yaml,metrics-profile.yaml" --tarball-name=metrics.tgz --start="$(date -d "-30 seconds" +%s)" --metrics-directory=${METRICS_FOLDER}
  check_file_list ${METRICS_FOLDER}/top2PrometheusCPU.json ${METRICS_FOLDER}/top2PrometheusCPU-start.json ${METRICS_FOLDER}/prometheusRSS.json
  run_cmd ${KUBE_BURNER} import --tarball=metrics.tgz --es-server=${ES_SERVER} --es-index=${ES_INDEX} --metrics-directory ${METRICS_FOLDER}
  rm -rf ${METRICS_FOLDER:?}/*
  run_cmd ${KUBE_BURNER} index --uuid=${UUID} -e metrics-endpoints.yaml --es-server=${ES_SERVER} --es-index=${ES_INDEX} --start="$(date -d "-30 seconds" +%s)" --metrics-directory ${METRICS_FOLDER}
  check_file_list ${METRICS_FOLDER}/top2PrometheusCPU.json ${METRICS_FOLDER}/prometheusRSS.json ${METRICS_FOLDER}/prometheusRSS.json
  rm -rf ${METRICS_FOLDER:?}/*
}

# bats test_tags=subsystem:job-type-delete
@test "kube-burner-delete.yml: delete=true; os-indexing=true; local-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-delete.yml --uuid ${UUID} --log-level=debug
  verify_object_count namespace 0 "" kube-burner.io/uuid=${UUID}
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/podLatencyMeasurement-delete-job.json ${METRICS_FOLDER}/podLatencyQuantilesMeasurement-delete-job.json ${METRICS_FOLDER}/prometheusBuildInfo.json
}

# bats test_tags=subsystem:job-type-read,subsystem:custom-kubeconfig
@test "kube-burner-read.yml: read; os-indexing=true; local-indexing=true" {
  export ES_INDEXING=true LOCAL_INDEXING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid=${UUID} --log-level=debug --kubeconfig="${TEST_KUBECONFIG}" --kube-context="${TEST_KUBECONTEXT}"
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS podLatencyMeasurement podLatencyQuantilesMeasurement
  check_file_list ${METRICS_FOLDER}/jobSummary.json ${METRICS_FOLDER}/prometheusBuildInfo.json
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

# bats test_tags=subsystem:waiters
@test "kube-burner.yml: waitOptions for Deployment" {
  export GC=false
  export WAIT_FOR_CONDITION="True"
  export WAIT_CUSTOM_STATUS_PATH='(.conditions.[] | select(.type == "Available")).status'
  run_cmd ${KUBE_BURNER} init -c  kube-burner.yml --uuid=${UUID} --log-level=debug
  check_custom_status_path kube-burner.io/uuid=${UUID} "{.items[*].status.conditions[].type}" Available
  ${KUBE_BURNER} destroy --uuid ${UUID}
}

# bats test_tags=subsystem:job-type-patch
@test "kube-burner-sequential-patch.yml: sequential patch" {
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
@test "kube-burner-virt-operations.yml: jobType kubevirt" {
  run_cmd ${KUBE_BURNER} init -c  kube-burner-virt-operations.yml --uuid=${UUID} --log-level=debug
}

# bats test_tags=subsystem:core
@test "kube-burner-userdata.yml: user data file" {
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
@test "kube-burner-dv.yml: datavolume latency" {
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

# bats test_tags=subsystem:indexing
@test "kube-burner-metrics-aggregate.yml: metrics aggregation" {
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

# bats test_tags=subsystem:measurements
@test "kube-burner-measurements.yml: Verify measurements configuration" {
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
