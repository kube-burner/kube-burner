#!/bin/bash
# vi: ft=bash
# shellcheck disable=SC2086,SC2068

set -e

# Default values
PARALLELISM=${PARALLELISM:-4}
KUBE_BURNER=${KUBE_BURNER:-kube-burner}

# Fix SC2035: Use ./* or -- * so names with dashes won't become options
chmod +x ./*.bats ./*.bash

# Source helper functions
source helpers.bash

# Function to run tests with proper quoting
run_tests() {
    local parallelism=$1
    local test_file=$2
    
    # Fix SC2086: Double quote to prevent globbing and word splitting
    KUBE_BURNER=$KUBE_BURNER bats -F pretty -T --print-output-on-failure -j "$parallelism" "$test_file"
}

# Check if we should run tests
if [[ "${SKIP_TESTS:-false}" == "true" ]]; then
    echo "Skipping tests as SKIP_TESTS is set to true"
    exit 0
fi

# Setup test environment
echo "Setting up test environment..."
setup_test_environment

# Run different test suites based on environment
if [[ "${TEST_SUITE:-k8s}" == "k8s" ]]; then
    echo "Running Kubernetes tests..."
    run_tests "$PARALLELISM" test-k8s.bats
elif [[ "${TEST_SUITE}" == "virt" ]]; then
    echo "Running virtualization tests..."
    run_tests "$PARALLELISM" test-virt.bats
else
    echo "Running default test suite..."
    # Fix SC2086: Quote the variable
    run_tests "$PARALLELISM" test-k8s.bats
fi

echo "Tests completed successfully!"
