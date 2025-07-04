#!/bin/bash
# Script to update your fork and create a PR to the upstream repository
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

# Ensure origin remote is properly set to your fork
echo "Setting origin remote to your fork..."
git remote set-url origin https://github.com/7908837174/kube-burner-kallal.git

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

# Ensure we're on the main branch
git checkout main

# Verify our commits
echo "===== LOCAL COMMITS ====="
git log -n 10 --oneline

# Push to your fork first
echo "===== PUSHING TO YOUR FORK ====="
echo "Attempting to push commits to origin/main (your fork)..."
git push --force origin main

echo "===== PUSH TO FORK COMPLETED ====="
git status

echo ""
echo "===== NEXT STEPS ====="
echo "1. Your commits have been pushed to your fork: https://github.com/7908837174/kube-burner-kallal"
echo "2. Now create a pull request from your fork to the main repository:"
echo "   https://github.com/kube-burner/kube-burner/compare/main...7908837174:kube-burner-kallal:main"
echo ""
echo "3. Or use GitHub CLI to create the PR (if you have it installed and configured):"
echo "   gh pr create --repo kube-burner/kube-burner --head 7908837174:kube-burner-kallal:main --base main \\"
echo "     --title \"Fix service checker pod setup and stabilize test infrastructure\" \\"
echo "     --body \"- Add file locking mechanism to prevent race conditions\n- Enhance netcat verification with multiple fallback methods\n- Fix pod deletion and creation logic with better diagnostics\n- Improve error handling with explicit failures\""
