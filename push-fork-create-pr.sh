#!/bin/bash
# Script to push commits to your fork and create a PR to the main repository
set -ex

# Navigate to the repository
cd /workspaces/kube-burner

# Configure git
git config --global user.name "Kube Burner CI"
git config --global user.email "kube-burner-ci@example.com"
git config --global --add safe.directory /workspaces/kube-burner

# Set up remotes correctly
echo "Setting up remotes..."
git remote set-url origin https://github.com/7908837174/kube-burner-kallal.git
if ! git remote | grep -q upstream; then
  git remote add upstream https://github.com/kube-burner/kube-burner.git
else
  git remote set-url upstream https://github.com/kube-burner/kube-burner.git
fi

# Show remote configuration
git remote -v

# Fetch latest from both remotes
echo "Fetching from remotes..."
git fetch origin
git fetch upstream

# Make sure we're on the main branch
echo "Checking out main branch..."
git checkout main

# Push your changes to your fork
echo "Pushing changes to your fork..."
git push --force origin main

echo "========================================"
echo "Your commits have been pushed to your fork:"
echo "https://github.com/7908837174/kube-burner-kallal"
echo ""
echo "To create a Pull Request to the main repository:"
echo "1. Go to: https://github.com/kube-burner/kube-burner/compare/main...7908837174:kube-burner-kallal:main"
echo "2. Click 'Create pull request'"
echo "3. Fill in the PR details"
echo "========================================"
