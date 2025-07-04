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

# Run with reduced parallelism for better stability
PARALLELISM="${PARALLELISM:-2}"
echo "Running full test suite with parallelism: $PARALLELISM"

# Execute the tests
KUBE_BURNER=$KUBE_BURNER bats -F pretty -T --print-output-on-failure -j "$PARALLELISM" test-k8s.bats
