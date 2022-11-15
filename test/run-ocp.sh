UUID=$(uuidgen)
COMMON_FLAGS="--es-server=${ES_SERVER} --es-index=${ES_INDEX} --alerting=false"

echo "Running node-density wrapper"
pushd ../config/node-density
kube-burner ocp node-density --pods-per-node=50 --pod-ready-threshold=10s --container-image=gcr.io/google_containers/pause:3.0 ${COMMON_FLAGS}
oc delete ns -l kube-burner-uuid=${UUID}
popd
echo "Running node-density-heavy wrapper"
pushd ../config/node-density-heavy
kube-burner ocp node-density-heavy --pods-per-node=40 ${COMMON_FLAGS}
oc delete ns -l kube-burner-uuid=${UUID}
popd
echo "Running cluster-density wrapper"
pushd ../config/cluster-density
kube-burner ocp node-density-heavy --iterations=3 ${COMMON_FLAGS}
oc delete ns -l kube-burner-uuid=${UUID} 
popd
