#!/bin/bash
# Enhanced push script with additional debugging and safe-guards

set -xe

# Navigate to the repo root
cd /workspaces/kube-burner

# Ensure git is properly configured
git config --global --add safe.directory /workspaces/kube-burner
git config --global user.email "kube-burner-ci@example.com" 
git config --global user.name "Kube Burner CI"

echo "===== Current git status ====="
git status

echo "===== Commit history ====="
git log -n 5 --oneline

echo "===== Pushing changes to main branch ====="
git push origin main

echo "===== Push completed ====="
