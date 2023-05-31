#!/usr/bin/env bats
# vi: ft=bash

setup_file() {
  export BATS_TEST_TIMEOUT=600
  export TEMP_FOLDER; TEMP_FOLDER=$(mktemp -d)
  export UUID; UUID=$(uuidgen)
  export JOB_ITERATIONS=5
  export QPS=2
  export BURST=2
  load helpers.bash
  setup-kind
  setup-prometheus
}

setup() {
  load helpers.bash
}

teardown() {
  oc delete ns -l kube-burner-uuid="${UUID}" --ignore-not-found
}

teardown_file() {
  load helpers.bash
  destroy-kind
  podman rm -f prometheus
}

@test "Running kube-burner: no indexing" {
  INDEXING=false kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
}

@test "Running kube-burner: indexing on. pod latency metrics" {
  INDEXING=true LATENCY_ONLY=true kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "Running kube-burner init: indexing on. pod latency mtrics and alert" {
  INDEXING=true kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -u http://localhost:9090 -m metrics-profile.yaml -a alert-profile.yaml
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "Running kube-burner init: indexing on. multiple endopoints" {
  INDEXING=true kube-burner init -c kube-burner.yml --uuid="${UUID}" --log-level=debug -e metrics-endpoints.yaml
  run test_init_checks
  [ "$status" -eq 0 ]
}

@test "Running kube-burner index: test with single prometheus endpoint" {
  INDEXING=true kube-burner index -c kube-burner-index-single-endpoint.yml -u http://localhost:9090 -m metrics-profile.yaml
}

@test "Running kube-burner index: test with metrics endpoints yaml" {
  INDEXING=true kube-burner index -c kube-burner.yml -e metrics-endpoints.yaml
  INDEXING=true kube-burner index -c kube-burner-index-multiple-endpoint.yml
}
