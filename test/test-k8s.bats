#!/usr/bin/env bats
# vi: ft=bash

load helpers.bash

setup_file() {
  export BATS_TEST_TIMEOUT=600
  export TEMP_FOLDER; TEMP_FOLDER=$(mktemp -d)
  export JOB_ITERATIONS=5
  export QPS=2
  export BURST=2
  setup-kind
  setup-prometheus
}

setup() {
  export UUID; UUID=$(uuidgen)
}

teardown() {
  kubectl delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

teardown_file() {
  load helpers.bash
  destroy-kind
  podman rm -f prometheus
}

@test "kube-burner: no indexing" {
  INDEXING=false kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  [ "$status" -eq 0 ]
}

@test "kube-burner: indexing on. pod latency metrics" {
  INDEXING=true LATENCY_ONLY=true kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  [ "$status" -eq 0 ]
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "kube-burner init: indexing, pod latency metrics and alerting" {
  INDEXING=true kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml -a alert-profile.yaml
  [ "$status" -eq 0 ]
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "kube-burner init: indexing and metrics-endopoints" {
  INDEXING=true kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -e metrics-endpoints.yaml
  [ "$status" -eq 0 ]
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "kube-burner index: metrics-endpoint with single prometheus endpoint" {
  INDEXING=true kube-burner index -c kube-burner-index-single-endpoint.yml -u http://localhost:9090 -m metrics-profile.yaml
  [ "$status" -eq 0 ]
}

@test "kube-burner index: test with metrics endpoints yaml" {
  INDEXING=true kube-burner index -c kube-burner.yml -e metrics-endpoints.yaml
  [ "$status" -eq 0 ]
  INDEXING=true kube-burner index -c kube-burner-index-multiple-endpoint.yml
  [ "$status" -eq 0 ]
}
