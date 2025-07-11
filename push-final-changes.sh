#!/bin/bash
set -e
set -x

# Navigate to repo root
cd /workspaces/kube-burner

# Add all the files we've modified
git add -f test/helpers.bash test/run-tests.sh

# Check if there are changes to commit
if git diff --cached --quiet; then
  echo "No changes to commit"
  exit 0
fi

# Commit with sign-off
git commit -s -m "Fix service checker pod setup and stabilize test infrastructure" \
  -m "- Add file locking mechanism for service checker setup to prevent race conditions" \
  -m "- Enhance netcat verification with multiple fallback methods for BusyBox compatibility" \
  -m "- Fix pod deletion and creation logic with better diagnostics" \
  -m "- Optimize resource requirements for svc-checker pod" \
  -m "- Fix shellcheck issues in test scripts" \
  -m "- Improve error handling and make failures explicit"

# Push to main branch
git push origin main
