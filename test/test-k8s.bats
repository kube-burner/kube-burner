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
  export GC=true
  export CHURN=false
  export CHURN_CYCLES=100
  export TEST_KUBECONFIG; TEST_KUBECONFIG=$(mktemp -d)/kubeconfig
  export TEST_KUBECONTEXT=test-context
  export ES_SERVER=${PERFSCALE_PROD_ES_SERVER:-"http://localhost:9200"}
  export ES_INDEX="kube-burner"
  export DEPLOY_GRAFANA=${DEPLOY_GRAFANA:-false}
  
  # K8S_VERSION is defined in helpers.bash
  # No special logic overrides K8S_VERSION - it's used exactly as provided
  # CI pipeline explicitly sets K8S_VERSION for each test matrix item
  
  if [[ "${USE_EXISTING_CLUSTER,,}" != "yes" ]]; then
    # Create the Kind cluster using the specified K8S version
    # If it fails, the entire test suite should fail
    setup-kind || {
      echo "FATAL: setup-kind failed"
      exit 1
    }
  fi
  create_test_kubeconfig
  setup-prometheus
  setup-service-checker
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
  export LOCAL_INDEXING=""
  export ALERTING=""
  export TIMESERIES_INDEXER=""
  
  # Re-create service checker before each test to ensure it's available
  # This prevents segfaults in service latency tests
  setup-service-checker
}

teardown() {
  echo "Cleaning up namespaces for test UUID ${UUID}"
  kubectl delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
  
  # Re-create service checker if it was deleted during the test
  # This ensures it's available for the next test
  if ! kubectl get namespace "${SERVICE_LATENCY_NS}" >/dev/null 2>&1; then
    echo "Service checker namespace not found, recreating..."
    setup-service-checker
  elif ! kubectl get pod -n "${SERVICE_LATENCY_NS}" "${SERVICE_CHECKER_POD}" >/dev/null 2>&1; then
    echo "Service checker pod not found, recreating..."
    setup-service-checker
  fi
}

teardown_file() {
  echo "Running teardown_file cleanup..."
  
  # Cleanup kubernetes resources if using a temporary cluster
  if [[ "${USE_EXISTING_CLUSTER,,}" != "yes" ]]; then
    echo "Destroying kind cluster..."
    destroy-kind || echo "Warning: destroy-kind failed but continuing with other cleanup"
  fi
  
  # Remove service checker namespace
  echo "Removing service checker namespace..."
  kubectl delete namespace ${SERVICE_LATENCY_NS} --grace-period=0 --force --ignore-not-found || \
    echo "Warning: Failed to remove service checker namespace ${SERVICE_LATENCY_NS}"

  # Remove prometheus container
  echo "Removing prometheus container..."
  $OCI_BIN rm -f prometheus || echo "Warning: Failed to remove prometheus container"
  
  # Cleanup OpenSearch and monitoring network if not using production ES server 
  # and grafana is not deployed
  if [[ -z "$PERFSCALE_PROD_ES_SERVER" ]]; then
    if [[ "$DEPLOY_GRAFANA" == "false" ]]; then
      echo "Removing opensearch container and monitoring network..."
      $OCI_BIN rm -f opensearch || echo "Warning: Failed to remove opensearch container"
      $OCI_BIN network rm -f monitoring || echo "Warning: Failed to remove monitoring network"
    fi
  fi
  
  echo "Teardown cleanup completed"
}

@test "kube-burner init: churn=true; absolute-path=true" {
  export CHURN=true
  export CHURN_CYCLES=2
  cp kube-burner.yml /tmp/kube-burner.yml
  run_cmd ${KUBE_BURNER} init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
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

@test "kube-burner init: os-indexing=true; local-indexing=true; vm-latency-indexing=true" {
  # Only skip if explicitly told to do so by SKIP_KUBEVIRT_TESTS
  # This ensures we catch real failures in CI instead of silently skipping
  if [[ "${SKIP_KUBEVIRT_TESTS:-false}" == "true" ]]; then
    skip "KubeVirt tests explicitly skipped by user (SKIP_KUBEVIRT_TESTS=true)"
  fi
  export ES_INDEXING=true LOCAL_INDEXING=true ALERTING=true
  run_cmd ${KUBE_BURNER} init -c kube-burner-virt.yml --uuid="${UUID}" --log-level=debug
  check_metric_value jobSummary top2PrometheusCPU prometheusRSS vmiLatencyMeasurement vmiLatencyQuantilesMeasurement alert
  check_file_list ${METRICS_FOLDER}/jobSummary.json  ${METRICS_FOLDER}/vmiLatencyMeasurement-kubevirt-density.json ${METRICS_FOLDER}/vmiLatencyQuantilesMeasurement-kubevirt-density.json
  check_destroyed_ns kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
  check_destroyed_pods default kube-burner-job=not-namespaced,kube-burner-uuid="${UUID}"
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

@test "kube-burner cluster health check" {
  run_cmd ${KUBE_BURNER} health-check
}

@test "kube-burner check-alerts" {
  run_cmd ${KUBE_BURNER} check-alerts -a alerts.yml -u http://localhost:9090 --metrics-directory=alerts
  check_file_list alerts/alert.json
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

@test "kube-burner init: sequential patch" {
  export NAMESPACE="sequential-patch"
  export LABEL_KEY="sequential.patch.test"
  export LABEL_VALUE_START="start"
  export LABEL_VALUE_END="end"
  export REPLICAS=50

  run_cmd ${KUBE_BURNER} init -c  kube-burner-sequential-patch.yml --uuid="${UUID}" --log-level=debug
  check_deployment_count ${NAMESPACE} ${LABEL_KEY} ${LABEL_VALUE_END} ${REPLICAS}
  kubectl delete ns ${NAMESPACE}
}

@test "kube-burner init: jobType kubevirt" {
  # Only skip if explicitly told to do so by SKIP_KUBEVIRT_TESTS
  # This ensures we catch real failures in CI instead of silently skipping
  if [[ "${SKIP_KUBEVIRT_TESTS:-false}" == "true" ]]; then
    skip "KubeVirt tests explicitly skipped by user (SKIP_KUBEVIRT_TESTS=true)"
  fi
  run_cmd ${KUBE_BURNER} init -c  kube-burner-virt-operations.yml --uuid="${UUID}" --log-level=debug
}

@test "kube-burner init: user data file" {
  export NAMESPACE="userdata"
  export deploymentLabelFromEnv="from-env"
  export REPLICAS=5

  run_cmd ${KUBE_BURNER} init -c kube-burner-userdata.yml --user-data=objectTemplates/userdata-test.yml --uuid="${UUID}" --log-level=debug
  # Verify that both labels were set
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-file" "unset" 0
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-env" "unset" 0
  # Verify that the from-file label was set from the user-data file
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-file" "from-file" ${REPLICAS}
  # Verify that the from-env label was NOT set from the user-data file
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-env" "from-file" 0
  # Verify that the from-env label was set from the environment variable
  check_deployment_count ${NAMESPACE} "kube-burner.io/from-env" "from-env" ${REPLICAS}
  # Verify that the default value is used when the variable is not set
  check_deployment_count ${NAMESPACE} "kube-burner.io/unset" "unset" ${REPLICAS}
  kubectl delete ns ${NAMESPACE}
}

@test "kube-burner init: datavolume latency" {
  # Only skip if explicitly told to do so by SKIP_KUBEVIRT_TESTS
  # This ensures we catch real failures in CI instead of silently skipping
  if [[ "${SKIP_KUBEVIRT_TESTS:-false}" == "true" ]]; then
    skip "KubeVirt tests explicitly skipped by user (SKIP_KUBEVIRT_TESTS=true)"
  fi
  
  # Only skip if explicitly told to do so by SKIP_CDI_TESTS
  # This ensures we catch real failures in CI instead of silently skipping
  if [[ "${SKIP_CDI_TESTS:-false}" == "true" ]]; then
    skip "CDI tests explicitly skipped by user (SKIP_CDI_TESTS=true)"
  fi
  
  # Only skip if explicitly told to do so by SKIP_SNAPSHOTTER_TESTS
  # This ensures we catch real failures in CI instead of silently skipping
  if [[ "${SKIP_SNAPSHOTTER_TESTS:-false}" == "true" ]]; then
    skip "Snapshotter tests explicitly skipped by user (SKIP_SNAPSHOTTER_TESTS=true)"
  fi
  
  # We skip if required variables are not set - this is a user configuration issue
  if [[ -z "$VOLUME_SNAPSHOT_CLASS_NAME" ]]; then
    echo "VOLUME_SNAPSHOT_CLASS_NAME must be set when using USE_EXISTING_CLUSTER"
    skip "Test skipped: VOLUME_SNAPSHOT_CLASS_NAME not set (required user configuration)"
  fi
  export STORAGE_CLASS_NAME=${STORAGE_CLASS_NAME:-$STORAGE_CLASS_WITH_SNAPSHOT_NAME}
  if [[ -z "$STORAGE_CLASS_NAME" ]]; then
    echo "STORAGE_CLASS_NAME must be set when using USE_EXISTING_CLUSTER"
    skip "Test skipped: STORAGE_CLASS_NAME not set (required user configuration)"
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
  # Only skip if explicitly told to do so by SKIP_KUBEVIRT_TESTS
  # This ensures we catch real failures in CI instead of silently skipping
  if [[ "${SKIP_KUBEVIRT_TESTS:-false}" == "true" ]]; then
    skip "KubeVirt tests explicitly skipped by user (SKIP_KUBEVIRT_TESTS=true)"
  fi
  
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
