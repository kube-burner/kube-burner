#!/bin/bash
# Enhanced test wrapper script with better error handling

set -eo pipefail

# Make sure both potential binaries are executable
chmod +x ../bin/amd64/kube-burner 2>/dev/null || echo "No binary in bin/amd64/"
chmod +x /tmp/kube-burner 2>/dev/null || echo "No binary in /tmp/"

# Ensure all test scripts are executable
chmod +x ./*.bats ./*.bash

# Determine which binary to use
if [ -x "/tmp/kube-burner" ]; then
  echo "Using /tmp/kube-burner as test binary"
  export KUBE_BURNER="/tmp/kube-burner"
elif [ -x "../bin/amd64/kube-burner" ]; then
  echo "Using bin/amd64/kube-burner as test binary"
  export KUBE_BURNER="../bin/amd64/kube-burner"
  # Copy to /tmp as backup
  cp ../bin/amd64/kube-burner /tmp/kube-burner
  chmod +x /tmp/kube-burner
else
  echo "ERROR: No executable kube-burner binary found!"
  exit 1
fi

# Echo which binary we're using and its permissions
echo "Using kube-burner binary: $KUBE_BURNER"
ls -la "$KUBE_BURNER"

# Run tests with reduced parallelism for stability
echo "Running tests with KUBE_BURNER=$KUBE_BURNER"

# First try single test to verify basic functionality
if [[ "$1" == "--single" ]]; then
  echo "Running single test first to verify functionality..."
  KUBE_BURNER=$KUBE_BURNER bats -F pretty -T --print-output-on-failure \
    test-k8s.bats -f "@test \"kube-burner init: churn=true; absolute-path=true\""
  exit $?
fi

# Clean up any existing lock files from previous test runs
echo "Cleaning up any existing lock files..."
rm -rf /tmp/kube-burner-locks
mkdir -p /tmp/kube-burner-locks
chmod 777 /tmp/kube-burner-locks

# Generate a unique test run ID that will be used for all parallel tests
export TEST_RUN_ID="$(date +%s)-$$"
# Generate a unique UUID for test identification across parallel runs
export UUID="test-$(uuidgen | cut -c1-8)-${TEST_RUN_ID}"
echo "Test run ID: $TEST_RUN_ID"
echo "Test UUID: $UUID"

# Ensure proper permissions for temporary files
chmod 1777 /tmp 2>/dev/null || true

# Pre-pull the busybox image to avoid pull delays during tests
echo "Pre-pulling busybox image to speed up tests..."
${OCI_BIN} pull busybox:1.35-musl >/dev/null 2>&1 || true

# Set lower parallelism in CI environments to reduce resource contention
if [ -n "$CI" ] || [ -n "$GITHUB_ACTIONS" ]; then
  # In CI, use more conservative parallelism
  PARALLELISM="${PARALLELISM:-2}"
  echo "CI environment detected, using conservative parallelism: $PARALLELISM"
else
  # For local development, default to higher parallelism
  PARALLELISM="${PARALLELISM:-4}"
  echo "Running with parallelism: $PARALLELISM"
fi

# Add a small random delay between test starts to reduce contention
export BATS_TEST_DELAY="${BATS_TEST_DELAY:-0.5}"
echo "Using test delay of $BATS_TEST_DELAY seconds between parallel tests"

# Set a higher timeout for tests in CI
if [ -n "$CI" ] || [ -n "$GITHUB_ACTIONS" ]; then
  export BATS_TEST_TIMEOUT="${BATS_TEST_TIMEOUT:-600}"
  echo "Setting higher test timeout for CI: $BATS_TEST_TIMEOUT seconds"
fi

# Function to clean up at exit
cleanup() {
  echo "Cleaning up test resources..."
  # Remove any lock files created by this test run
  find /tmp/kube-burner-locks -name "*${TEST_RUN_ID}*" -delete 2>/dev/null || true
  
  # Clean up any orphaned service checker pods
  if command -v kubectl >/dev/null 2>&1; then
    kubectl delete pod -n kube-burner-service-latency svc-checker --grace-period=0 --force --ignore-not-found 2>/dev/null || true
  fi
  
  echo "Cleanup completed"
}

# Register cleanup function
trap cleanup EXIT

# Execute the tests with appropriate parallelism
echo "Running test suite with KUBE_BURNER=$KUBE_BURNER and parallelism=$PARALLELISM"

# For the churn=true test specifically, run it separately first to ensure it works
if [ "$2" != "--skip-critical" ]; then
  echo "Running critical test first to verify basic functionality..."
  KUBE_BURNER=$KUBE_BURNER bats -F pretty -T --print-output-on-failure \
    test-k8s.bats -f "@test \"kube-burner init: churn=true; absolute-path=true\""
  echo "Critical test completed successfully, proceeding with full test suite"
fi

# Run the full test suite
KUBE_BURNER=$KUBE_BURNER bats -F pretty -T --print-output-on-failure -j "$PARALLELISM" test-k8s.bats
