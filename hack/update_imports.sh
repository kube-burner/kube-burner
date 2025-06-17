#!/usr/bin/env bash

# Script to update import paths in all Go files
# from github.com/kube-burner/kube-burner to github.com/cloud-bulldozer/kube-burner

set -e

# Define the search and replace strings
OLD_IMPORT="github.com/kube-burner/kube-burner"
NEW_IMPORT="github.com/cloud-bulldozer/kube-burner"

# Find all Go files and update the imports
find . -name "*.go" -type f -exec sed -i "s|${OLD_IMPORT}|${NEW_IMPORT}|g" {} \;

echo "Import paths updated in all Go files"
