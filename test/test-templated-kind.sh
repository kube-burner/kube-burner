#!/bin/bash

set -e

cd /workspaces/kube-burner
echo "Building kube-burner..."
go build -o bin/kube-burner ./cmd/kube-burner

# Use the existing kind cluster
echo "Using the test-kube-burner cluster..."
kubectl config use-context kind-test-kube-burner
kubectl cluster-info

# Run the test
echo "Running test for templated kind field..."
cd /workspaces/kube-burner/test/k8s
../../bin/kube-burner init -c kube-burner-templated-kind.yml --uuid="test-$(date +%s)" --log-level=debug

# Verify CRDs creation - wait for them to be established
for i in {1..2}; do
  echo "Checking for CRD testcrds${i}.testcrd.example.com..."
  if kubectl get crd "testcrds${i}.testcrd.example.com" &>/dev/null; then
    echo "SUCCESS: CRD testcrds${i}.testcrd.example.com was created successfully"
    
    # Check if the CRD is established by checking its status conditions
    established=$(kubectl get crd "testcrds${i}.testcrd.example.com" -o jsonpath='{.status.conditions[?(@.type=="Established")].status}')
    if [ "$established" == "True" ]; then
      echo "CRD testcrds${i}.testcrd.example.com is established"
    else
      echo "WARNING: CRD testcrds${i}.testcrd.example.com exists but is not yet established"
      kubectl get crd "testcrds${i}.testcrd.example.com" -o yaml | grep -A10 status
    fi
  else
    echo "FAILURE: CRD testcrds${i}.testcrd.example.com was not created"
    exit 1
  fi
done

# Verify CR creation
for i in {1..2}; do
  echo "Checking for CR test1 of kind TestCRD${i}..."
  if kubectl get "testcrds${i}" test1 &>/dev/null; then
    echo "SUCCESS: CR test1 of kind TestCRD${i} was created successfully"
  else
    echo "FAILURE: CR test1 of kind TestCRD${i} was not created"
    echo "Listing all CRDs:"
    kubectl get customresourcedefinitions
    echo "Checking kube-burner logs:"
    kubectl logs -l app=kube-burner --tail=50
    exit 1
  fi
done

# Clean up
echo "Cleaning up..."
kubectl delete crd testcrds1.testcrd.example.com testcrds2.testcrd.example.com --ignore-not-found

echo "Test completed successfully!"
