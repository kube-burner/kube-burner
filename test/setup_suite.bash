setup_suite() {
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

teardown_suite() {
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
