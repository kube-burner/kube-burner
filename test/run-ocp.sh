#!/bin/bash

source base.sh

setup-prometheus
trap 'die' ERR

die() {
  if [[ -n $1 ]]; then
    echo $1
  fi
  oc delete ns -l kube-burner-uuid=${UUID} --ignore-not-found
  exit 1
}

UUID=$(uuidgen)
ES_SERVER="https://search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com/"
ES_INDEX="kube-burner-ocp"
COMMON_FLAGS="--es-server=${ES_SERVER} --es-index=${ES_INDEX} --alerting=true --uuid=${UUID} --qps=5 --burst=5"

echo "Running node-density wrapper"
kube-burner ocp node-density --pods-per-node=75 --pod-ready-threshold=10s --container-image=gcr.io/google_containers/pause:3.0 ${COMMON_FLAGS}
hits=$(curl "${ES_SERVER}/${ES_INDEX}/_search?q=uuid.keyword:${UUID}+AND+metricName.keyword:etcdVersion" | jq .hits.total.value)
if [[ ${hits} == 0 ]]; then
  die "Couldn't find the metric etcdVersion indexed in ES"
fi
echo "Running node-density-heavy wrapper"
kube-burner ocp node-density-heavy --pods-per-node=75 ${COMMON_FLAGS} --qps=5 --burst=5
echo "Running cluster-density wrapper and user metadata"
kube-burner ocp cluster-density --iterations=2 --churn-duration=2m ${COMMON_FLAGS} --user-metadata=user-metadata.yml
echo "Running cluster-density wrapper"
kube-burner ocp cluster-density --iterations=2 --churn=false --uuid=${UUID}
echo "Running cluster-density wrapper for multiple endpoints case"
kube-burner ocp cluster-density --iterations=1 --churn-duration=2m --metrics-endpoint metrics-endpoints.yaml ${COMMON_FLAGS}
echo "Running cluster-density-v2 wrapper"
kube-burner ocp cluster-density-v2 --iterations=2 --churn-duration=2m ${COMMON_FLAGS}
# Disable gc and avoid metric indexing
echo "Running node-density-cni wrapper with gc=false"
kube-burner ocp node-density-cni --pods-per-node=75 --gc=false --uuid=${UUID} --alerting=false
oc delete ns -l kube-burner-uuid=${UUID}
trap - ERR
echo "Running cluster-density timeout case"
kube-burner ocp cluster-density --iterations=1 --churn-duration=5m ${COMMON_FLAGS} --timeout=1s
if [ ${?} != 2 ]; then
  die "Kube-burner timed out but its exit code was ${rc} != 2"
fi
