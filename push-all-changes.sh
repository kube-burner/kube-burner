#!/bin/bash
# Script to push all changes to the main branch

# Set error handling
set -e

# Print step information
echo "====== PUSHING ALL CHANGES TO MAIN BRANCH ======"
echo "Current directory: $(pwd)"

# Ensure we're in the repository root
cd /workspaces/kube-burner

# Configure git if needed
if [ -z "$(git config --get user.email)" ]; then
  echo "Setting git user email..."
  git config --global user.email "kube-burner-ci@example.com"
fi

if [ -z "$(git config --get user.name)" ]; then
  echo "Setting git user name..."
  git config --global user.name "Kube Burner CI"
fi

# Add all the modified files
echo "Adding files to git..."
git add test/helpers.bash test/run-tests.sh

# Check if there are changes to be committed
git_status=$(git status --porcelain)
if [ -z "$git_status" ]; then
  echo "No changes to commit. Files might already be committed."
else
  echo "Changes detected, proceeding with commit..."
  
  # Create the commit with sign-off and detailed message
  echo "Creating commit..."
  git commit -s -m "Fix service checker pod setup and stabilize test infrastructure" \
    -m "- Add file locking mechanism to prevent race conditions in parallel test runs" \
    -m "- Enhance netcat verification with multiple fallback methods for BusyBox compatibility" \
    -m "- Fix pod deletion and creation logic with better error diagnostics" \
    -m "- Optimize resource requirements for service checker pod" \
    -m "- Improve error handling with explicit failures instead of silent skipping" \
    -m "- Fix shellcheck issues in test scripts"
  
  echo "Commit created successfully"
fi

# Push the changes
echo "Pushing changes to main branch..."
git push origin main

echo "====== PUSH COMPLETED ======"

# Show the latest commit
echo "Latest commit:"
git log -1 --oneline
