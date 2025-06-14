#!/bin/bash

set -e

cd /workspaces/kube-burner/test/mini-test

# Build and run the YAML generator
echo "Building and running YAML generator..."
go build -o generate-yamls ./generate-test-yamls.go
./generate-yamls

echo "Rendered YAML files:"
ls -la rendered/
echo

# Use the existing kind cluster
echo "Using the test-kube-burner cluster..."
kubectl config use-context kind-test-kube-burner
kubectl cluster-info

# Apply the CRD first and wait
echo "Applying CRD..."
kubectl apply -f rendered/crd.yaml
echo "Waiting for CRD to be established..."
sleep 2

# Check CRD status
established=$(kubectl get crd testcrds1.testcrd.example.com -o jsonpath='{.status.conditions[?(@.type=="Established")].status}' 2>/dev/null || echo "NotFound")
if [ "$established" == "True" ]; then
  echo "SUCCESS: CRD testcrds1.testcrd.example.com is established"
else
  echo "WARNING: CRD may not be fully established, continuing anyway..."
  kubectl get crd testcrds1.testcrd.example.com -o yaml
fi

# Apply the CR
echo "Applying CR..."
kubectl apply -f rendered/cr.yaml

# Verify CR creation
echo "Verifying CR was created..."
if kubectl get testcrds1 test1 &>/dev/null; then
  echo "SUCCESS: CR test1 was created successfully"
else
  echo "FAILURE: CR test1 was not created"
  kubectl get customresourcedefinitions
  exit 1
fi

# Clean up
echo "Cleaning up..."
kubectl delete -f rendered/cr.yaml --ignore-not-found
kubectl delete -f rendered/crd.yaml --ignore-not-found

echo "Test completed successfully!"
