#!/usr/bin/env bash

# Default version if git commands fail
DEFAULT_VERSION="v1.0.0-dev"

if [[ -z $(git branch --show-current 2>/dev/null) ]]; then
  # Try to get the latest tag, fall back to default if it fails
  git describe --tags --abbrev=0 2>/dev/null || echo "${DEFAULT_VERSION}"
else
  # Current branch name or default if command fails
  git branch --show-current 2>/dev/null | sed 's/master/latest/g' || echo "${DEFAULT_VERSION}"
fi
