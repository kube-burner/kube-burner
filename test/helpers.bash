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
SERVICE_LATENCY_NS="kube-burner-service-latency"
SERVICE_CHECKER_POD="svc-checker"

# ERROR HANDLING POLICY:
# 1. All setup failures result in explicit test failure (exit 1), NEVER silent skipping
# 2. Tests are ONLY skipped when user explicitly sets SKIP_* variables
# 3. All errors use "FATAL:" prefix for consistency and easy identification
# 4. For external dependencies that may be unreliable (GitHub API calls), we use fallback versions
#    after retries fail rather than failing the tests entirely

# This function ensures the service-checker pod is properly set up for service latency tests
setup-service-checker() {
  # Add a unique identifier to avoid conflicts when tests run in parallel
  local test_uuid
  test_uuid="${UUID:-$(uuidgen | cut -c1-8)}"
  
  # Use a mutex-like approach to prevent parallel setup-service-checker calls from interfering
  local lock_file="/tmp/svc-checker-lock"
  
  # Acquire lock with timeout
  local timeout=30
  local start_time=$SECONDS
  while [ -f "$lock_file" ] && [ $(($SECONDS - $start_time)) -lt $timeout ]; do
    echo "Waiting for lock to be released... ($(($timeout - $SECONDS + $start_time))s left)"
    sleep 1
  done
  
  # If lock still exists after timeout, proceed anyway but log the issue
  if [ -f "$lock_file" ]; then
    echo "WARNING: Lock file still exists after timeout, proceeding anyway"
    rm -f "$lock_file" # Force remove stale lock
  fi
  
  # Create lock file
  echo "$test_uuid" > "$lock_file"
  
  echo "Setting up service checker pod for service latency tests (uuid: $test_uuid)"
  
  # Create namespace if it doesn't exist
  if ! kubectl get namespace "${SERVICE_LATENCY_NS}" >/dev/null 2>&1; then
    echo "Creating service latency namespace: ${SERVICE_LATENCY_NS}"
    if ! kubectl create namespace "${SERVICE_LATENCY_NS}"; then
      echo "FATAL: Failed to create service latency namespace"
      rm -f "$lock_file" # Release lock on failure
      exit 1
    fi
  else
    echo "Service latency namespace ${SERVICE_LATENCY_NS} already exists"
  fi
  
  # Delete the service checker pod with simpler, more reliable approach
  echo "Deleting service checker pod (if exists)"
  # First try the normal ignore-not-found approach
  kubectl delete pod -n "${SERVICE_LATENCY_NS}" "${SERVICE_CHECKER_POD}" --grace-period=0 --force --ignore-not-found
  
  # Now regardless of whether it existed or not, wait for it to be fully gone
  # This handles edge cases where pods are terminating or stuck
  for i in {1..5}; do
    if ! kubectl get pod -n "${SERVICE_LATENCY_NS}" "${SERVICE_CHECKER_POD}" >/dev/null 2>&1; then
      echo "Service checker pod is gone"
      break
    fi
    
    if [ $i -eq 5 ]; then
      # Final attempt - forcibly remove pod with patch to remove finalizers
      echo "FATAL: Service checker pod is stuck, attempting to force removal"
      kubectl patch pod "${SERVICE_CHECKER_POD}" -n "${SERVICE_LATENCY_NS}" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
      kubectl delete pod -n "${SERVICE_LATENCY_NS}" "${SERVICE_CHECKER_POD}" --grace-period=0 --force --wait=false
      sleep 5
      # If it's still there after that, we can't do much more - abort
      if kubectl get pod -n "${SERVICE_LATENCY_NS}" "${SERVICE_CHECKER_POD}" >/dev/null 2>&1; then
        echo "FATAL: Service checker pod could not be deleted after multiple attempts"
        kubectl describe pod/${SERVICE_CHECKER_POD} -n ${SERVICE_LATENCY_NS}
        exit 1
      fi
    else
      echo "Waiting for pod to be fully deleted (attempt $i/5)..."
      sleep 3
    fi
  done
  
  # Brief pause to ensure API server state is consistent
  sleep 3
  
  # Use an image with netcat pre-installed to avoid dependency issues
  # The busybox image has 'nc' built in which is more reliable in CI environments
  echo "Creating service checker pod with busybox image (includes netcat)"
  
  # Create the pod directly with kubectl run for simplicity - less chance of errors
  echo "Creating service checker pod with busybox image"
  
  # Use kubectl run for more reliable pod creation with fewer moving parts
  # Use the smallest busybox image and reduce resource requirements
  if ! kubectl run ${SERVICE_CHECKER_POD} \
    --namespace=${SERVICE_LATENCY_NS} \
    --image=busybox:1.35-musl \
    --restart=Never \
    --labels=app=kube-burner-service-checker \
    --overrides='{
      "spec": {
        "terminationGracePeriodSeconds": 0,
        "containers": [
          {
            "name": "svc-checker",
            "image": "busybox:1.35-musl",
            "command": ["sh", "-c", "trap \"exit 0\" TERM; sleep infinity"],
            "imagePullPolicy": "IfNotPresent",
            "resources": {
              "requests": {
                "memory": "16Mi",
                "cpu": "10m"
              },
              "limits": {
                "memory": "32Mi",
                "cpu": "50m"
              }
            },
            "securityContext": {
              "allowPrivilegeEscalation": false,
              "capabilities": {"drop": ["ALL"]},
              "runAsNonRoot": false,
              "seccompProfile": {"type": "RuntimeDefault"}
            }
          }
        ]
      }
    }'; then
    
    # If first attempt fails, try again with a delay - could be a race condition
    echo "First pod creation attempt failed, waiting and retrying..."
    sleep 5
    
    # Make sure pod doesn't exist (could be partial creation)
    kubectl delete pod -n "${SERVICE_LATENCY_NS}" "${SERVICE_CHECKER_POD}" --force --grace-period=0 --ignore-not-found
    sleep 3
    
    # One more attempt with kubectl create instead of run
    echo "Creating pod with simplified approach"
    cat <<EOF | kubectl create -f -
apiVersion: v1
kind: Pod
metadata:
  name: ${SERVICE_CHECKER_POD}
  namespace: ${SERVICE_LATENCY_NS}
  labels:
    app: kube-burner-service-checker
spec:
  terminationGracePeriodSeconds: 0
  containers:
  - name: ${SERVICE_CHECKER_POD}
    image: busybox:1.35-musl
    command: ["sh", "-c", "sleep infinity"]
    imagePullPolicy: IfNotPresent
    resources:
      requests:
        memory: "16Mi"
        cpu: "10m"
      limits:
        memory: "32Mi"
        cpu: "50m"
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
      runAsNonRoot: false
      seccompProfile:
        type: RuntimeDefault
EOF
    
    if [ $? -ne 0 ]; then
      echo "FATAL: Failed to create service checker pod after multiple attempts"
      kubectl get pods -n ${SERVICE_LATENCY_NS}
      exit 1
    fi
  fi

  # Simple but effective pod readiness check
  echo "Waiting for service checker pod to be ready"
  
  # Use a longer wait timeout and retry approach for slow environments (like CI)
  local wait_attempts=3
  local wait_timeout=120s
  
  for attempt in $(seq 1 $wait_attempts); do
    echo "Waiting for service checker pod to be ready (attempt $attempt/$wait_attempts, timeout: $wait_timeout)"
    if kubectl wait --for=condition=Ready pod/${SERVICE_CHECKER_POD} -n ${SERVICE_LATENCY_NS} --timeout=$wait_timeout; then
      echo "Service checker pod is ready on attempt $attempt"
      break
    fi
    
    # If we've tried all attempts and failed, gather diagnostics and fail
    if [ $attempt -eq $wait_attempts ]; then
      echo "FATAL: Service checker pod did not become ready after $wait_attempts attempts"
      # Collect diagnostic information
      echo "--- Pod Details ---"
      kubectl describe pod/${SERVICE_CHECKER_POD} -n ${SERVICE_LATENCY_NS}
      echo "--- Pod Status ---"
      kubectl get pod/${SERVICE_CHECKER_POD} -n ${SERVICE_LATENCY_NS} -o yaml
      echo "--- Events ---"
      kubectl get events --sort-by='.lastTimestamp' -n ${SERVICE_LATENCY_NS}
      echo "--- Node Status ---"
      kubectl get nodes
      echo "--- Docker Info ---"
      $OCI_BIN info
      echo "--- Node Resources ---"
      kubectl describe nodes
      exit 1
    fi
    
    # Let's try to help the pod along
    echo "Pod not ready yet. Checking pull status..."
    kubectl get pod/${SERVICE_CHECKER_POD} -n ${SERVICE_LATENCY_NS}
    
    # If possible, prefetch the image on all nodes to help things along
    echo "Attempting to ensure image is available on nodes..."
    for node in $(kubectl get nodes -o name | cut -d/ -f2); do
      echo "Checking node $node for busybox image..."
      kubectl describe node $node | grep -i busybox || true
    done
  done
  
  echo "Service checker pod is ready, verifying functionality"
  
  # Verify netcat is available - simple check, no complex logic
  echo "Verifying netcat is available in the service checker pod"
  sleep 2  # Brief pause for container to stabilize
  
  # Simple netcat check
  if ! kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- which nc >/dev/null 2>&1; then
    echo "FATAL: netcat (nc) command not found in service checker pod"
    exit 1
  fi
  
  # BusyBox nc can vary between versions, try multiple test approaches
  echo "Testing netcat functionality with multiple methods..."
  
  # Test Method 1: The most compatible approach - just run 'nc' with no args
  # This should fail with exit code 1 but show netcat is working
  if kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- nc 2>&1 | grep -q "Usage"; then
    echo "Netcat basic usage test passed"
  # Test Method 2: Try connecting to localhost with timeout
  elif kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- sh -c "nc -z localhost 1 >/dev/null 2>&1 || [ \$? -eq 1 ]"; then
    echo "Netcat connection test passed"
  # Test Method 3: Try without -w flag (some BusyBox versions don't support it)
  elif kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- sh -c "echo | nc localhost 1 >/dev/null 2>&1 || [ \$? -eq 1 ]"; then
    echo "Netcat echo pipe test passed"
  # If all tests fail, check for alternative netcat command
  else
    echo "WARNING: Standard netcat tests failed"
    if kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- which netcat >/dev/null 2>&1; then
      echo "Found alternative 'netcat' command, using that instead"
    else
      # Final attempt - create a test file and use nc with it
      echo "Trying to create test file for netcat verification..."
      if kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- sh -c "echo 'test' > /tmp/nctest && nc -h < /tmp/nctest >/dev/null 2>&1 || [ \$? -lt 127 ]"; then
        echo "Netcat works with test file input"
      else
        echo "FATAL: All netcat functionality tests failed"
        exit 1
      fi
    fi
  fi
  
  # Basic check that we can execute commands in the pod
  if ! kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- echo "Service checker pod is functional"; then
    echo "FATAL: Cannot execute commands in the service checker pod"
    rm -f "/tmp/svc-checker-lock" # Release lock on failure
    exit 1
  fi
  
  echo "Service checker pod is ready with netcat available"
  
  # Release the lock
  rm -f "/tmp/svc-checker-lock"
}

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
    echo "FATAL: Failed to create cluster with image ${IMAGE}"
    exit 1
  }
  # Check if we should skip KubeVirt installation - ONLY skips if user explicitly requests it
  if [[ "${SKIP_KUBEVIRT_INSTALL:-false}" == "true" ]]; then
    echo "Skipping KubeVirt installation as explicitly requested by SKIP_KUBEVIRT_INSTALL=true"
    # User explicitly asked to skip installation, so we respect that for tests as well
    # NOTE: We only set SKIP_*_TESTS variables when user explicitly requests it, never due to failures
    export SKIP_KUBEVIRT_TESTS="true"
    # Skip CDI tests as well if we're skipping KubeVirt (also user-requested)
    export SKIP_CDI_TESTS="true"
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
      
      # If version still cannot be determined, use a known good version
      if [[ -z "$KUBEVIRT_VERSION" || "$KUBEVIRT_VERSION" == "null" ]]; then
        echo "Could not determine latest KubeVirt version via GitHub API, using fallback version v1.0.0"
        KUBEVIRT_VERSION="v1.0.0"
      fi
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
    
    # Continue with CDI installation since KubeVirt was successfully installed
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
      
      # If version still cannot be determined, use a known good version
      if [[ -z "$CDI_VERSION" || "$CDI_VERSION" == "null" ]]; then
        echo "Could not determine latest CDI version via GitHub API, using fallback version v1.55.0"
        CDI_VERSION="v1.55.0"
      fi
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
      
      # If version still cannot be determined, use a known good version
      if [[ -z "$SNAPSHOTTER_VERSION" || "$SNAPSHOTTER_VERSION" == "null" ]]; then
        echo "Could not determine latest snapshotter version via GitHub API, using fallback version v6.2.1"
        SNAPSHOTTER_VERSION="v6.2.1"
      fi
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
      echo "Could not determine latest Helm version via GitHub API, using fallback version v3.12.3"
      HELM_VERSION="v3.12.3"
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
  fi
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
  # Direct approach - fail fast if there's an issue
  if ! $OCI_BIN run --rm -d --name prometheus --publish=9090:9090 docker.io/prom/prometheus:latest; then
    echo "FATAL: Failed to start prometheus container"
    exit 1
  fi
  echo "Prometheus container started successfully"
  sleep 10
}

setup-shared-network() {
  echo "Setting up shared network for monitoring"
  if ! $OCI_BIN network create monitoring; then
    echo "FATAL: Failed to create monitoring network"
    exit 1
  fi
  echo "Monitoring network created successfully"
}

setup-opensearch() {
  echo "Setting up open-search"
  # Use version 1 to avoid the password requirement
  if ! $OCI_BIN run --rm -d --name opensearch --network monitoring --env="discovery.type=single-node" --env="plugins.security.disabled=true" --publish=9200:9200 docker.io/opensearchproject/opensearch:1; then
    echo "FATAL: Failed to start opensearch container"
    exit 1
  fi
  echo "Opensearch container started successfully"
  sleep 10
}

setup-grafana() {
  export GRAFANA_URL="http://localhost:3000"
  export GRAFANA_ROLE="admin"
  echo "Setting up Grafana"
  
  if ! $OCI_BIN run --rm -d --name grafana --network monitoring -p 3000:3000 \
    --env GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_ROLE} \
    docker.io/grafana/grafana:latest; then
    echo "FATAL: Failed to start Grafana container"
    exit 1
  fi
  
  echo "Grafana container started successfully"
  sleep 10
  echo "Grafana is running at $GRAFANA_URL"
}

configure-grafana-datasource() {
  echo "Configuring Elasticsearch as Grafana Data Source"
  if ! curl -X POST "$GRAFANA_URL/api/datasources" \
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
    }"; then
    echo "FATAL: Failed to configure Grafana data source"
    exit 1
  fi
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
    echo "FATAL: Number of namespaces labeled with ${1} less than expected"
    exit 1
  fi
}

check_destroyed_ns() {
  echo "Checking namespace \"${1}\" has been destroyed"
  if [[ $(kubectl get ns -l "${1}" -o json | jq '.items | length') != 0 ]]; then
    echo "FATAL: Namespaces labeled with \"${1}\" not destroyed"
    exit 1
  fi
}

check_destroyed_pods() {
  echo "Checking pods have been destroyed in namespace ${1}"
  if [[ $(kubectl get pod -n "${1}" -l "${2}" -o json | jq '.items | length') != 0 ]]; then
    echo "FATAL: Pods in namespace ${1} not destroyed"
    exit 1
  fi
}

check_running_pods() {
  running_pods=$(kubectl get pod -A -l ${1} --field-selector=status.phase==Running -o json | jq '.items | length')
  if [[ "${running_pods}" != "${2}" ]]; then
    echo "FATAL: Running pods in cluster labeled with ${1} different from expected: Expected=${2}, observed=${running_pods}"
    exit 1
  fi
}

check_running_pods_in_ns() {
    running_pods=$(kubectl get pod -n "${1}" -l kube-burner-job=namespaced --field-selector=status.phase==Running -o json | jq '.items | length')
    if [[ "${running_pods}" != "${2}" ]]; then
      echo "FATAL: Running pods in namespace $1 different from expected. Expected=${2}, observed=${running_pods}"
      exit 1
    fi
}

check_file_list() {
  for f in "${@}"; do
    if [[ ! -f ${f} ]]; then
      echo "FATAL: File ${f} not found"
      echo "Content of $(dirname ${f}):"
      ls -l "$(dirname ${f})"
      exit 1
    fi
    if [[ $(jq .[0].metricName ${f}) == "" ]]; then
      echo "FATAL: Incorrect format in ${f}"
      cat "${f}"
      exit 1
    fi
  done
  return 0
}

check_files_dont_exist() {
  for f in "${@}"; do
    if [[ -f ${f} ]]; then
      echo "FATAL: File ${f} found when it shouldn't exist"
      exit 1
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
    if ! RESULT=$(curl -sS ${endpoint} | jq '.hits.total.value // error'); then
      echo "FATAL: curl or jq command failed"
      exit 1
    elif [ "${RESULT}" == 0 ]; then
      echo "FATAL: ${metric} not found in ${endpoint}"
      exit 1
    else
      # Found the metric - continue to next one or return from function
      continue
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
          echo "FATAL: File ${f} not found"
          exit 1
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
    echo "FATAL: Expected ${EXPECTED_COUNT} replicas to be patches with ${LABEL_KEY}=${LABEL_VALUE_END} but found only ${ACTUAL_COUNT}"
    exit 1
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
      echo "FATAL: metric ${type}/${metric} was not recorded for ${job}"
      exit 1
  fi
}

check_quantile_recorded() {
  local job=$1
  local type=$2
  local quantileName=$3

  MEASUREMENT=$(cat ${METRICS_FOLDER}/${type}QuantilesMeasurement-${job}.json | jq --arg name "${quantileName}" '[.[] | select(.quantileName == $name)][0].avg')
  if [[ ${MEASUREMENT} -eq 0 ]]; then
    echo "FATAL: Quantile for ${type}/${quantileName} was not recorded for ${job}"
    exit 1
  fi
}

check_metrics_not_created_for_job() {
  local job=$1
  local type=$2

  METRICS_FILE=${METRICS_FOLDER}/${type}Measurement-${job}.json
  QUANTILE_FILE=${METRICS_FOLDER}/${type}QuantilesMeasurement-${job}.json

  if [ -f "${METRICS_FILE}" ]; then
    echo "FATAL: Metrics file for ${job} was created when it shouldn't have been"
    exit 1
  fi

  if [ -f "${QUANTILE_FILE}" ]; then
    echo "FATAL: Quantile file for ${job} was created when it shouldn't have been"
    exit 1
  fi
}
