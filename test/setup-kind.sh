echo "Downloading kind"
curl -LsSO https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-linux-amd64
chmod +x kind-linux-amd64
echo "Deploying cluster"
./kind-linux-amd64 create cluster --config kind.yml --image kindest/node:${K8S_VERSION} --name kind --wait 300s -v=1
