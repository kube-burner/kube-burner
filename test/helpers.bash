#!/bin/bash
# vi: ft=bash
# shellcheck disable=SC2086,SC2068

KIND_VERSION=${KIND_VERSION:-v0.19.0}
K8S_VERSION=${K8S_VERSION:-v1.31.0}
OCI_BIN=${OCI_BIN:-podman}
ARCH=$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)
KUBE_BURNER=${KUBE_BURNER:-kube-burner}

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
    IMAGE=kindest/node:"${K8S_VERSION}"
  fi
  echo "Deploying cluster"
  "${KIND_FOLDER}/kind-linux-${ARCH}" create cluster --config kind.yml --image ${IMAGE} --name kind --wait 300s -v=1
  echo "Deploying kubevirt operator"
  KUBEVIRT_VERSION=$(curl -s https://api.github.com/repos/kubevirt/kubevirt/releases/latest | jq -r .tag_name)
  kubectl create -f https://github.com/kubevirt/kubevirt/releases/download/"${KUBEVIRT_VERSION}"/kubevirt-operator.yaml
  kubectl create -f objectTemplates/kubevirt-cr.yaml
  kubectl wait --for=condition=Available --timeout=600s -n kubevirt deployments/virt-operator
  kubectl wait --for=condition=Available --timeout=600s -n kubevirt kv/kubevirt
  # Install CDI
  CDI_VERSION=$(basename "$(curl -s -w '%{redirect_url}' https://github.com/kubevirt/containerized-data-importer/releases/latest)")
  kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-operator.yaml
  kubectl create -f https://github.com/kubevirt/containerized-data-importer/releases/download/${CDI_VERSION}/cdi-cr.yaml
  kubectl wait --for=condition=Available --timeout=600s cdi cdi
  # Install Snapshot CRDs and Controller
  SNAPSHOTTER_VERSION=$(curl -s https://api.github.com/repos/kubernetes-csi/external-snapshotter/releases/latest | jq -r .tag_name)
  kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
  kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
  kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml
  kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
  kubectl apply -f https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/${SNAPSHOTTER_VERSION}/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml
  # Add Host Path CSI Driver and StorageClass
  CSI_DRIVER_HOST_PATH_DIR=$(mktemp -d)
  git clone https://github.com/kubernetes-csi/csi-driver-host-path.git ${CSI_DRIVER_HOST_PATH_DIR}
  ${CSI_DRIVER_HOST_PATH_DIR}/deploy/kubernetes-latest/deploy.sh
  kubectl apply -f ${CSI_DRIVER_HOST_PATH_DIR}/examples/csi-storageclass.yaml
  # Install Helm
  HELM_VERSION=$(basename "$(curl -s -w '%{redirect_url}' https://github.com/helm/helm/releases/latest)")
  curl -LsS https://get.helm.sh/helm-${HELM_VERSION}-linux-${ARCH}.tar.gz -o ${KIND_FOLDER}/helm.tgz
  tar xzvf ${KIND_FOLDER}/helm.tgz -C ${KIND_FOLDER}
  HELM_EXEC=${KIND_FOLDER}/linux-${ARCH}/helm
  chmod +x ${HELM_EXEC}
  # Install K10
  ${HELM_EXEC} repo add kasten https://charts.kasten.io/
  kubectl create ns kasten-io
  ${HELM_EXEC} install k10 kasten/k10 --namespace=kasten-io
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
  $OCI_BIN run --rm -d --name prometheus --publish=9090:9090 docker.io/prom/prometheus:latest
  sleep 10
}

setup-shared-network() {
  echo "Setting up shared network for monitoring"
  $OCI_BIN network create monitoring
}

setup-opensearch() {
  echo "Setting up open-search"
  # Use version 1 to avoid the password requirement
  $OCI_BIN run --rm -d --name opensearch --network monitoring --env="discovery.type=single-node" --env="plugins.security.disabled=true" --publish=9200:9200 docker.io/opensearchproject/opensearch:1
  sleep 10
}

setup-grafana() {
  export GRAFANA_URL="http://localhost:3000"
  export GRAFANA_ROLE="admin"
  echo "Setting up Grafana"
  $OCI_BIN run --rm -d --name grafana --network monitoring -p 3000:3000 \
    --env GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_ROLE} \
    docker.io/grafana/grafana:latest
  sleep 10
  echo "Grafana is running at $GRAFANA_URL"
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
  running_pods=$(kubectl get pod -A -l "${1}" --field-selector=status.phase==Running -o json | jq '.items | length')
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

check_running_custom_resources_in_ns() {
  running_resources=$(kubectl get "${1}" -n "${2}" -l kube-burner-job=namespaced -o json | jq '.items | length')
  if [[ "${running_resources}" != "${3}" ]]; then
    echo "Running ${1}s in namespace $2 different from expected. Expected=${3}, observed=${running_resources}"
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
