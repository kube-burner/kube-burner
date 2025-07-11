#!/bin/bash
# Script to push all changes to the main branch with detailed debugging

set -xe

# Navigate to the repo root
cd /workspaces/kube-burner

# Configure git if needed
git config --global --add safe.directory /workspaces/kube-burner
git config --global user.email "kube-burner-ci@example.com"
git config --global user.name "Kube Burner CI"

# Show current status
echo "===== CURRENT GIT STATUS ====="
git status

# Show recent commits
echo "===== RECENT COMMITS ====="
git log -n 6 --oneline

# Push all commits to main
echo "===== PUSHING TO MAIN ====="
git push origin main

echo "===== PUSH COMPLETED ====="
