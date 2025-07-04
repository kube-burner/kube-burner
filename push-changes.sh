#!/bin/bash
set -e
set -x

# Navigate to repo root
cd /workspaces/kube-burner

# Add all the files we've modified
git add test/helpers.bash test/run-tests.sh pkg/measurements/util/svc_checker.go

# Commit with sign-off
git commit -s -m "Fix service checker pod netcat compatibility and shellcheck issues in test scripts"

# Push to main branch
git push origin main
