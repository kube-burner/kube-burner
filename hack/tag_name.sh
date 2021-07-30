#!/usr/bin/env bash

if [[ -z $(git branch --show-current) ]]; then
  git describe --tags
else
  git branch --show-current | sed 's/master/latest/g'
fi
