#!/bin/bash
echo "Starting push script"
cd /workspaces/kube-burner || { echo "Failed to change directory"; exit 1; }

echo "Current directory: $(pwd)"
echo "Git status:"
git status

echo "Adding files:"
git add -f test/helpers.bash test/run-tests.sh
echo "Git status after adding files:"
git status

echo "Attempting to commit:"
git commit -s -m "Fix service checker pod setup and stabilize test infrastructure" \
  -m "- Add file locking mechanism for service checker setup to prevent race conditions" \
  -m "- Enhance netcat verification with multiple fallback methods for BusyBox compatibility" \
  -m "- Fix pod deletion and creation logic with better diagnostics" \
  -m "- Optimize resource requirements for svc-checker pod" \
  -m "- Fix shellcheck issues in test scripts" \
  -m "- Improve error handling and make failures explicit"

echo "Attempting to push:"
git push origin main

echo "Script completed"
