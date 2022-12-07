#!/bin/bash

trap 'cleanup' ERR

cleanup() {
  oc delete ns -l kube-burner-uuid=${UUID} --ignore-not-found
  exit 1
}

UUID=$(uuidgen)
ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com/"
ES_INDEX="kube-burner-ocp"
COMMON_FLAGS="--es-server=${ES_SERVER} --es-index=${ES_INDEX} --alerting=false --uuid=${UUID} --qps=5 --burst=5"

echo "Running node-density wrapper"
kube-burner ocp node-density --pods-per-node=75 --pod-ready-threshold=10s --container-image=gcr.io/google_containers/pause:3.0 ${COMMON_FLAGS}
echo "Running node-density-heavy wrapper"
kube-burner ocp node-density-heavy --pods-per-node=75 ${COMMON_FLAGS} --qps=5 --burst=5
echo "Running cluster-density wrapper"
kube-burner ocp cluster-density --iterations=3 --churn-duration=5m ${COMMON_FLAGS}
# Disable gc and avoid metric indexing
echo "Running node-density-cni wrapper"
kube-burner ocp node-density-cni --pods-per-node=75 --gc=false --uuid=${UUID}
oc delete ns -l kube-burner-uuid=${UUID}
