#!/bin/bash
# Script to force push all commits to the main branch

# Set error handling
set -e

# Print step information
echo "====== FORCE PUSHING ALL COMMITS TO MAIN BRANCH ======"
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

# Get current branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
echo "Current branch: $CURRENT_BRANCH"

# Get ahead/behind information
AHEAD_COUNT=$(git rev-list --count HEAD..origin/$CURRENT_BRANCH 2>/dev/null || echo "0")
BEHIND_COUNT=$(git rev-list --count origin/$CURRENT_BRANCH..HEAD 2>/dev/null || echo "0")

echo "Your branch is behind origin/$CURRENT_BRANCH by $AHEAD_COUNT commits"
echo "Your branch is ahead of origin/$CURRENT_BRANCH by $BEHIND_COUNT commits"

# Try regular push first
echo "Attempting regular push..."
git push origin $CURRENT_BRANCH && {
  echo "✅ Regular push successful!"
  exit 0
} || echo "Regular push failed, trying force push..."

# Try force push with lease (safer)
echo "Attempting force push with lease..."
git push --force-with-lease origin $CURRENT_BRANCH && {
  echo "✅ Force push with lease successful!"
  exit 0
} || echo "Force push with lease failed, trying direct force push..."

# Last resort: direct force push
echo "Attempting direct force push..."
git push --force origin $CURRENT_BRANCH && {
  echo "✅ Direct force push successful!"
  exit 0
} || {
  echo "❌ All push attempts failed!"
  echo "Please check your repository permissions and remote configuration."
  exit 1
}
