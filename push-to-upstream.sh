#!/bin/bash
# Script to push commits from local fork to upstream main repository
set -e

echo "===== SETTING UP REPOSITORIES ====="
cd /workspaces/kube-burner

# Configure git
git config --global user.name "Kube Burner CI"
git config --global user.email "kube-burner-ci@example.com"
git config --global --add safe.directory /workspaces/kube-burner

# Display current remotes
echo "Current remotes:"
git remote -v

# Ensure upstream remote is configured properly
if ! git remote get-url upstream &>/dev/null; then
  echo "Adding upstream remote..."
  git remote add upstream https://github.com/kube-burner/kube-burner.git
else
  echo "Updating upstream remote URL..."
  git remote set-url upstream https://github.com/kube-burner/kube-burner.git
fi

# Update remotes
echo "Fetching from remotes..."
git fetch origin
git fetch upstream

# Show branch status
echo "===== BRANCH STATUS ====="
git branch -vv
echo "Commits ahead/behind upstream/main:"
git rev-list --count --left-right origin/main...upstream/main

# Ensure we're on the main branch
git checkout main

# Verify our commits
echo "===== LOCAL COMMITS ====="
git log -n 10 --oneline

# Push to upstream (this requires proper authentication)
echo "===== PUSHING TO UPSTREAM ====="
echo "Attempting to push commits to upstream/main..."
# Note: This requires proper GitHub authentication
git push upstream main

echo "===== PUSH COMPLETED ====="
git status
