#!/bin/bash
# vi: ft=bash
# shellcheck disable=SC2086,SC2068

KIND_VERSION=${KIND_VERSION:-v0.19.0}
# Central definition of K8S_VERSION used by all tests
# This value is used as the default but may be overridden by environment variables
# In CI, this is overridden by the workflow matrix to test multiple K8S versions
K8S_VERSION=${K8S_VERSION:-v1.31.0}
OCI_BIN=${OCI_BIN:-docker}
ARCH=$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)
KUBE_BURNER=${KUBE_BURNER:-kube-burner}
# Set CI mode flag to true in CI environments to handle infrastructure transient errors
# This adds retries for environment setup, not for the actual tests
CI_MODE=${CI_MODE:-false}

setup-kind() {
  KIND_FOLDER=$(mktemp -d)
  echo "Downloading kind"
  # Kind is currently unavailable for ppc64le architecture, it is required that the binary is built for usage.
  if [[ "$ARCH" == "ppc64le" ]]
  then
    git clone --single-branch --filter=tree:0 --branch ${KIND_VERSION} https://github.com/kubernetes-sigs/kind.git
    make -C kind/ install INSTALL_DIR="${KIND_FOLDER}" KIND_BINARY_NAME="kind-linux-${ARCH}"
    IMAGE=quay.io/powercloud/kind-node:"${K8S_VERSION}"
  else
    curl -LsS https://github.com/kubernetes-sigs/kind/releases/download/"${KIND_VERSION}/kind-linux-${ARCH}" -o ${KIND_FOLDER}/kind-linux-${ARCH}
    chmod +x ${KIND_FOLDER}/kind-linux-${ARCH}
    # Use the specified K8S_VERSION node image
    IMAGE=kindest/node:"${K8S_VERSION}"
  fi
  
  echo "Deploying cluster"
  # Create the kind cluster with the specified image
  "${KIND_FOLDER}/kind-linux-${ARCH}" create cluster --config kind.yml --image ${IMAGE} --name kind --wait 300s -v=1 || { 
    echo "Error: Failed to create cluster with image ${IMAGE}"
    exit 1
  }
  # Check if we should skip KubeVirt installation
  if [[ "${SKIP_KUBEVIRT_INSTALL:-false}" == "true" ]]; then
    echo "Skipping KubeVirt installation as explicitly requested by SKIP_KUBEVIRT_INSTALL=true"
    # User explicitly asked to skip installation, so we respect that for tests as well
    export SKIP_KUBEVIRT_TESTS="true"
  else
    echo "Deploying KubeVirt operator"
    # Determine KubeVirt version with retries
    if [[ -z "$KUBEVIRT_VERSION" ]]; then
      # Retry up to 3 times with increasing delays
      for retry in 1 2 3; do
        echo "Fetching latest KubeVirt version (attempt $retry/3)..."
        KUBEVIRT_VERSION=$(curl -s -m 10 https://api.github.com/repos/kubevirt/kubevirt/releases/latest | jq -r .tag_name)
        if [[ -n "$KUBEVIRT_VERSION" && "$KUBEVIRT_VERSION" != "null" ]]; then
          break
        fi
        echo "Failed to fetch KubeVirt version, retrying in $retry seconds..."
        sleep $retry
      done
    fi
    
    # If version cannot be determined, fail the tests
    if [[ -z "$KUBEVIRT_VERSION" || "$KUBEVIRT_VERSION" == "null" ]]; then
      echo "Error: Could not determine KubeVirt version after multiple attempts"
      echo "FATAL: Failed to determine KubeVirt version"
      exit 1
    fi
    
    echo "Using KubeVirt version: $KUBEVIRT_VERSION"
    
    # Install KubeVirt with proper error checking - fail test if installation fails
    echo "Installing KubeVirt operator..."
    if ! kubectl create -f https://github.com/kubevirt/kubevirt/releases/download/"${KUBEVIRT_VERSION}"/kubevirt-operator.yaml; then
      echo "FATAL: Failed to install KubeVirt operator"
      exit 1
    fi
    
    echo "Creating KubeVirt CR..."
    if ! kubectl create -f objectTemplates/kubevirt-cr.yaml; then
      echo "FATAL: Failed to create KubeVirt CR"
      exit 1
    fi
    
    echo "Waiting for KubeVirt operator to be available..."
    if ! kubectl wait --for=condition=Available --timeout=600s -n kubevirt deployments/virt-operator; then
      echo "FATAL: KubeVirt operator did not become available"
      exit 1
    fi
    
    echo "Waiting for KubeVirt CR to be available..."
    if ! kubectl wait --for=condition=Available --timeout=600s -n kubevirt kv/kubevirt; then
      echo "FATAL: KubeVirt CR did not become available"
      exit 1
    fi
    
    echo "KubeVirt successfully installed and available"
  fi
  # Install CDI if KubeVirt was successfully installed
  if [[ "${SKIP_KUBEVIRT_TESTS:-false}" == "true" ]]; then
    echo "Skipping CDI installation as KubeVirt installation was explicitly skipped via SKIP_KUBEVIRT_INSTALL=true"
    # Only propagate the skip if it was due to user explicitly requesting to skip install
    if [[ "${SKIP_KUBEVIRT_INSTALL:-false}" == "true" ]]; then
      export SKIP_CDI_TESTS="true"
    else
      echo "FATAL: Cannot install CDI due to KubeVirt installation failure"
      exit 1
    fi
    return
  fi

  echo "Installing CDI..."
  
  # Determine CDI version with retries if not specified
  if [[ -z "$CDI_VERSION" ]]; then
    # Retry up to 3 times with increasing delays
    for retry in 1 2 3; do
      echo "Fetching latest CDI version (attempt $retry/3)..."
      # Try first method: redirect URL
      CDI_VERSION=$(basename "$(curl -s -m 10 -w '%{redirect_url}' https://github.com/kubevirt/containerized-data-importer/releases/latest)" 2>/dev/null || echo "")
      
      # If first method fails, try second method: Github API
      if [[ -z "$CDI_VERSION" || "$CDI_VERSION" == "null" ]]; then
        CDI_VERSION=$(curl -s -m 10 https://api.github.com/repos/kubevirt/containerized-data-importer/releases/latest | jq -r .tag_name 2>/dev/null || echo "")
      fi
      
      if [[ -n "$CDI_VERSION" && "$CDI_VERSION" != "null" ]]; then
        break
      fi
      
      echo "Failed to fetch CDI version, retrying in $retry seconds..."
      sleep $retry
    done
  fi
  
  # If version cannot be determined, fail the tests
  if [[ -z "$CDI_VERSION" || "$CDI_VERSION" == "null" ]]; then
    echo "FATAL: Could not determine CDI version after multiple attempts"
    exit 1
  fi
  
  echo "Using CDI version: $CDI_VERSION"
  
  echo "Installing CDI operator..."
  if ! kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-operator.yaml; then
    echo "FATAL: Failed to install CDI operator"
    exit 1
  fi
  
  echo "Creating CDI CR..."
  if ! kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-cr.yaml; then
    echo "FATAL: Failed to create CDI CR"
    exit 1
  fi
  
  echo "Waiting for CDI to be available..."
  if ! kubectl wait --for=condition=Available --timeout=600s cdi cdi; then
    echo "FATAL: CDI did not become available"
    exit 1
  fi
  
  echo "CDI successfully installed and available"
  # Install Snapshot CRDs and Controller
  # Determine snapshotter version with retries if not specified
  if [[ -z "$SNAPSHOTTER_VERSION" ]]; then
    # Retry up to 3 times with increasing delays
    for retry in 1 2 3; do
      echo "Fetching latest snapshotter version (attempt $retry/3)..."
      SNAPSHOTTER_VERSION=$(curl -s -m 10 https://api.github.com/repos/kubernetes-csi/external-snapshotter/releases/latest | jq -r .tag_name)
      
      if [[ -n "$SNAPSHOTTER_VERSION" && "$SNAPSHOTTER_VERSION" != "null" ]]; then
        break
      fi
      
      echo "Failed to fetch snapshotter version, retrying in $retry seconds..."
      sleep $retry
    done
  fi
  
  # If version cannot be determined, fail the tests
  if [[ -z "$SNAPSHOTTER_VERSION" || "$SNAPSHOTTER_VERSION" == "null" ]]; then
    echo "FATAL: Could not determine snapshotter version after multiple attempts"
    exit 1
  fi
  
  echo "Using snapshotter version: $SNAPSHOTTER_VERSION"
  
  # Install snapshotter components with proper error checking - fail on installation errors
  if ! kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml; then
    echo "FATAL: Failed to install volumesnapshotclasses CRD"
    exit 1
  fi
  
  if ! kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml; then
    echo "FATAL: Failed to install volumesnapshotcontents CRD"
    exit 1
  fi
  
  if ! kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml; then
    echo "FATAL: Failed to install volumesnapshots CRD"
    exit 1
  fi
  
  if ! kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml; then
    echo "FATAL: Failed to install snapshot controller RBAC"
    exit 1
  fi
  
  if ! kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml; then
    echo "FATAL: Failed to install snapshot controller"
    exit 1
  fi
  
  echo "Snapshot controller and CRDs successfully installed"
  # Add Host Path CSI Driver and StorageClass
  echo "Installing Host Path CSI Driver..."
  CSI_DRIVER_HOST_PATH_DIR=$(mktemp -d)
  if ! git clone --quiet https://github.com/kubernetes-csi/csi-driver-host-path.git ${CSI_DRIVER_HOST_PATH_DIR}; then
    echo "FATAL: Failed to clone csi-driver-host-path repository"
    exit 1
  fi
  
  if ! ${CSI_DRIVER_HOST_PATH_DIR}/deploy/kubernetes-latest/deploy.sh; then
    echo "FATAL: Failed to deploy Host Path CSI Driver"
    exit 1
  fi
  
  if ! kubectl apply -f ${CSI_DRIVER_HOST_PATH_DIR}/examples/csi-storageclass.yaml; then
    echo "FATAL: Failed to apply CSI StorageClass"
    exit 1
  fi
  
  # Install Helm
  echo "Installing Helm..."
  HELM_VERSION=""
  for retry in 1 2 3; do
    echo "Fetching latest Helm version (attempt $retry/3)..."
    HELM_VERSION=$(basename "$(curl -s -w '%{redirect_url}' https://github.com/helm/helm/releases/latest)")
    if [[ -n "$HELM_VERSION" && "$HELM_VERSION" != "null" ]]; then
      break
    fi
    echo "Failed to fetch Helm version, retrying in $retry seconds..."
    sleep $retry
  done
  
  if [[ -z "$HELM_VERSION" || "$HELM_VERSION" == "null" ]]; then
    echo "FATAL: Could not determine Helm version after multiple attempts"
    exit 1
  fi
  
  if ! curl -LsS https://get.helm.sh/helm-${HELM_VERSION}-linux-${ARCH}.tar.gz -o ${KIND_FOLDER}/helm.tgz; then
    echo "FATAL: Failed to download Helm"
    exit 1
  fi
  
  if ! tar xzf ${KIND_FOLDER}/helm.tgz -C ${KIND_FOLDER}; then
    echo "FATAL: Failed to extract Helm archive"
    exit 1
  fi
  
  HELM_EXEC=${KIND_FOLDER}/linux-${ARCH}/helm
  chmod +x ${HELM_EXEC}
  
  # Install K10
  echo "Installing K10..."
  if ! ${HELM_EXEC} repo add kasten https://charts.kasten.io/; then
    echo "FATAL: Failed to add Kasten Helm repository"
    exit 1
  fi
  
  if ! kubectl create ns kasten-io; then
    echo "FATAL: Failed to create kasten-io namespace"
    exit 1
  fi
  
  if ! ${HELM_EXEC} install k10 kasten/k10 --namespace=kasten-io; then
    echo "FATAL: Failed to install K10 via Helm"
    exit 1
  fi
  
  echo "K10 successfully installed"
  export STORAGE_CLASS_WITH_SNAPSHOT_NAME="csi-hostpath-sc"
  export VOLUME_SNAPSHOT_CLASS_NAME="csi-hostpath-snapclass"
}

create_test_kubeconfig() {
  echo "Creating another kubeconfig"
  if [[ "${USE_EXISTING_CLUSTER,,}" == "yes" ]]; then
    EXISTING_KUBECONFIG=${KUBECONFIG:-"~/.kube/config"}
    cp ${EXISTING_KUBECONFIG} ${TEST_KUBECONFIG}
    EXISTING_CONTEXT=$(kubectl config current-context)
  else
    "${KIND_FOLDER}/kind-linux-${ARCH}" export kubeconfig --kubeconfig "${TEST_KUBECONFIG}"
    EXISTING_CONTEXT="kind-kind"
  fi
  kubectl config rename-context ${EXISTING_CONTEXT} "${TEST_KUBECONTEXT}" --kubeconfig "${TEST_KUBECONFIG}"
}

destroy-kind() {
  echo "Destroying kind cluster"
  "${KIND_FOLDER}/kind-linux-${ARCH}" delete cluster
}

setup-prometheus() {
  echo "Setting up prometheus instance"
  # Retry the prometheus container start in CI mode
  if [[ "$CI_MODE" == "true" ]]; then
    for retry in 1 2 3; do
      echo "Starting prometheus container (attempt $retry/3)..."
      if $OCI_BIN run --rm -d --name prometheus --publish=9090:9090 docker.io/prom/prometheus:latest; then
        echo "Prometheus container started successfully"
        sleep 10
        return 0
      fi
      echo "Failed to start prometheus container, retrying in $retry seconds..."
      $OCI_BIN rm -f prometheus 2>/dev/null || true
      sleep $retry
    done
    echo "Error: Could not start prometheus container after multiple attempts"
    return 1
  else
    # Standard mode - no retries
    $OCI_BIN run --rm -d --name prometheus --publish=9090:9090 docker.io/prom/prometheus:latest
    sleep 10
  fi
}

setup-shared-network() {
  echo "Setting up shared network for monitoring"
  # Retry network creation in CI mode
  if [[ "$CI_MODE" == "true" ]]; then
    for retry in 1 2 3; do
      echo "Creating monitoring network (attempt $retry/3)..."
      if $OCI_BIN network create monitoring; then
        echo "Monitoring network created successfully"
        return 0
      fi
      echo "Failed to create monitoring network, retrying in $retry seconds..."
      $OCI_BIN network rm -f monitoring 2>/dev/null || true
      sleep $retry
    done
    echo "Error: Could not create monitoring network after multiple attempts"
    return 1
  else
    # Standard mode - no retries
    $OCI_BIN network create monitoring
  fi
}

setup-opensearch() {
  echo "Setting up open-search"
  # Use version 1 to avoid the password requirement
  # Retry opensearch container start in CI mode
  if [[ "$CI_MODE" == "true" ]]; then
    for retry in 1 2 3; do
      echo "Starting opensearch container (attempt $retry/3)..."
      if $OCI_BIN run --rm -d --name opensearch --network monitoring --env="discovery.type=single-node" --env="plugins.security.disabled=true" --publish=9200:9200 docker.io/opensearchproject/opensearch:1; then
        echo "Opensearch container started successfully"
        sleep 10
        return 0
      fi
      echo "Failed to start opensearch container, retrying in $retry seconds..."
      $OCI_BIN rm -f opensearch 2>/dev/null || true
      sleep $retry
    done
    echo "Error: Could not start opensearch container after multiple attempts"
    return 1
  else
    # Standard mode - no retries
    $OCI_BIN run --rm -d --name opensearch --network monitoring --env="discovery.type=single-node" --env="plugins.security.disabled=true" --publish=9200:9200 docker.io/opensearchproject/opensearch:1
    sleep 10
  fi
}

setup-grafana() {
  export GRAFANA_URL="http://localhost:3000"
  export GRAFANA_ROLE="admin"
  echo "Setting up Grafana"
  
  # Retry grafana container start in CI mode
  if [[ "$CI_MODE" == "true" ]]; then
    for retry in 1 2 3; do
      echo "Starting Grafana container (attempt $retry/3)..."
      if $OCI_BIN run --rm -d --name grafana --network monitoring -p 3000:3000 \
          --env GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_ROLE} \
          docker.io/grafana/grafana:latest; then
        echo "Grafana container started successfully"
        sleep 10
        echo "Grafana is running at $GRAFANA_URL"
        return 0
      fi
      echo "Failed to start Grafana container, retrying in $retry seconds..."
      $OCI_BIN rm -f grafana 2>/dev/null || true
      sleep $retry
    done
    echo "Error: Could not start Grafana container after multiple attempts"
    return 1
  else
    # Standard mode - no retries
    $OCI_BIN run --rm -d --name grafana --network monitoring -p 3000:3000 \
      --env GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_ROLE} \
      docker.io/grafana/grafana:latest
    sleep 10
    echo "Grafana is running at $GRAFANA_URL"
  fi
}

configure-grafana-datasource() {
  echo "Configuring Elasticsearch as Grafana Data Source"
  curl -X POST "$GRAFANA_URL/api/datasources" \
    -H "Content-Type: application/json" \
    -H "Authorization: Basic $(echo -n "$GRAFANA_ROLE:$GRAFANA_ROLE" | base64)" \
    --data "{
      \"name\": \"kube-burner elasticsearch\",
      \"type\": \"elasticsearch\",
      \"url\": \"http://opensearch:9200\",
      \"access\": \"proxy\",
      \"database\": \"${ES_INDEX}\",
      \"jsonData\": {
        \"timeField\": \"timestamp\"
      }
    }"
}

deploy-grafana-dashboards() {
  echo "Deploying Grafana dashboards from JSON files"
  for json_file in ../grafana/*.json; do
    dashboard_json=$(cat "$json_file")
    curl -s -X POST "$GRAFANA_URL/api/dashboards/db" \
      -H "Content-Type: application/json" \
      -H "Authorization: Basic $(echo -n "$GRAFANA_ROLE:$GRAFANA_ROLE" | base64)" \
      --data "{
        \"dashboard\": $dashboard_json,
        \"overwrite\": true
      }"
  done
}

check_ns() {
  echo "Checking the number of namespaces labeled with \"${1}\" is \"${2}\""
  if [[ $(kubectl get ns -l "${1}" -o json | jq '.items | length') != "${2}" ]]; then
    echo "Number of namespaces labeled with ${1} less than expected"
    return 1
  fi
}

check_destroyed_ns() {
  echo "Checking namespace \"${1}\" has been destroyed"
  if [[ $(kubectl get ns -l "${1}" -o json | jq '.items | length') != 0 ]]; then
    echo "Namespaces labeled with \"${1}\" not destroyed"
    return 1
  fi
}

check_destroyed_pods() {
  echo "Checking pods have been destroyed in namespace ${1}"
  if [[ $(kubectl get pod -n "${1}" -l "${2}" -o json | jq '.items | length') != 0 ]]; then
    echo "Pods in namespace ${1} not destroyed"
    return 1
  fi
}

check_running_pods() {
  running_pods=$(kubectl get pod -A -l ${1} --field-selector=status.phase==Running -o json | jq '.items | length')
  if [[ "${running_pods}" != "${2}" ]]; then
    echo "Running pods in cluster labeled with ${1} different from expected: Expected=${2}, observed=${running_pods}"
    return 1
  fi
}

check_running_pods_in_ns() {
    running_pods=$(kubectl get pod -n "${1}" -l kube-burner-job=namespaced --field-selector=status.phase==Running -o json | jq '.items | length')
    if [[ "${running_pods}" != "${2}" ]]; then
      echo "Running pods in namespace $1 different from expected. Expected=${2}, observed=${running_pods}"
      return 1
    fi
}

check_file_list() {
  for f in "${@}"; do
    if [[ ! -f ${f} ]]; then
      echo "File ${f} not found"
      echo "Content of $(dirname ${f}):"
      ls -l "$(dirname ${f})"
      return 1
    fi
    if [[ $(jq .[0].metricName ${f}) == "" ]]; then
      echo "Incorrect format in ${f}"
      cat "${f}"
      return 1
    fi
  done
  return 0
}

check_files_dont_exist() {
  for f in "${@}"; do
    if [[ -f ${f} ]]; then
      echo "File ${f} found"
      return 1
    fi
  done
  return 0
}


print_events() {
  kubectl get events --sort-by='.lastTimestamp' -A
}

check_custom_status_path() {
  label=$1
  statusPath=$2
  expectedValue=$3

  # Get the status path for all deployments matching the label in all namespaces
  results=$(kubectl get deployment -l "$label" -A -o jsonpath="$statusPath")

  # Loop through each result and check if it matches the expected value
  for result in $results; do
      echo "$result"
    if [[ "$result" != "$expectedValue" ]]; then
      echo "Custom status path did not match expected value: $expectedValue"
      exit 1
    fi
  done

  echo "All status paths match the expected value: $expectedValue"
}

check_metric_value() {
  sleep 3 # There's some delay on the documents to show up in OpenSearch
  for metric in "${@}"; do
    endpoint="${ES_SERVER}/${ES_INDEX}/_search?q=uuid.keyword:${UUID}+AND+metricName.keyword:${metric}"
    RESULT=$(curl -sS ${endpoint} | jq '.hits.total.value // error')
    RETURN_CODE=$?
    if [ "${RETURN_CODE}" -ne 0 ]; then
      echo "Return code: ${RETURN_CODE}"
      return 1
    elif [ "${RESULT}" == 0 ]; then
      echo "${metric} not found in ${endpoint}"
      return 1
    else
      return 0
    fi
  done
}

run_cmd(){
  echo "$@"
  ${@}
}

check_file_exists() {
  for f in "${@}"; do
      if [[ ! -f ${f} ]]; then
          echo "File ${f} not found"
          return 1
      fi
  done
  return 0
}

check_deployment_count() {
  local NAMESPACE=${1}
  local LABEL_KEY=${2}
  local LABEL_VALUE=${3}
  local EXPECTED_COUNT=${4}

  ACTUAL_COUNT=$(kubectl get deployment -n ${NAMESPACE} -l ${LABEL_KEY}=${LABEL_VALUE} -o json | jq '.items | length')
  if [[ "${ACTUAL_COUNT}" != "${EXPECTED_COUNT}" ]]; then
    echo "Expected ${EXPECTED_COUNT} replicas to be patches with ${LABEL_KEY}=${LABEL_VALUE_END} but found only ${ACTUAL_COUNT}"
    return 1
  fi
  echo "Found the expected ${EXPECTED_COUNT} deployments"
}

get_default_storage_class() {
    kubectl get sc -o json | jq -r '[.items.[] | select(.metadata.annotations."storageclass.kubernetes.io/is-default-class")][0].metadata.name'
}

check_metric_recorded() {
  local job=$1
  local type=$2
  local metric=$3
  local m
  m=$(cat ${METRICS_FOLDER}/${type}Measurement-${job}.json | jq .[0].${metric})
  if [[ ${m} -eq 0 ]]; then
      echo "metric ${type}/${metric} was not recorded for ${job}"
      return 1
  fi
}

check_quantile_recorded() {
  local job=$1
  local type=$2
  local quantileName=$3

  MEASUREMENT=$(cat ${METRICS_FOLDER}/${type}QuantilesMeasurement-${job}.json | jq --arg name "${quantileName}" '[.[] | select(.quantileName == $name)][0].avg')
  if [[ ${MEASUREMENT} -eq 0 ]]; then
    echo "Quantile for ${type}/${quantileName} was not recorded for ${job}"
    return 1
  fi
}

check_metrics_not_created_for_job() {
  local job=$1
  local type=$2

  METRICS_FILE=${METRICS_FOLDER}/${type}Measurement-${job}.json
  QUANTILE_FILE=${METRICS_FOLDER}/${type}QuantilesMeasurement-${job}.json

  if [ -f "${METRICS_FILE}" ]; then
    echo "Metrics file for ${job} was created"
    return 1
  fi

  if [ -f "${QUANTILE_FILE}" ]; then
    echo "Quantile file for ${job} was created"
    return 1
  fi
}
