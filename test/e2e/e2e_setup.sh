#!/bin/bash -ex

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
CURRENT_DIR="$(cd ${CURRENT_DIR} && pwd)"

export PATH=$PATH:${CURRENT_DIR}/bin

image_repository="${image_repository:-quay.io/stolostron}"
image_name="${image_name:-cloudevents-conductor}"
image_tag="${image_tag:-$(date +%s)}"
managed_cluster_name="cluster1"

kind_version=0.29.0
if ! command -v kind >/dev/null 2>&1; then
    echo "This script will install kind (https://kind.sigs.k8s.io/) on your machine."
    curl -Lo ./kind-amd64 "https://kind.sigs.k8s.io/dl/v${kind_version}/kind-$(uname)-amd64"
    chmod +x ./kind-amd64
    sudo mv ./kind-amd64 /usr/local/bin/kind
fi

# 1. create KinD cluster
export KUBECONFIG=${CURRENT_DIR}/.kubeconfig
if [ ! -f "$KUBECONFIG" ]; then
  cat << EOF | kind create cluster --name cloudevents-conductor-e2e --kubeconfig ${KUBECONFIG} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30080
    hostPort: 30080
  - containerPort: 30090
    hostPort: 30090
EOF
fi

# 2. build conductor image and load to KinD cluster
image_tag=${image_tag} BASE_IMAGE=golang:1.24 make image
  # related issue: https://github.com/kubernetes-sigs/kind/issues/2038
if command -v docker &> /dev/null; then
    kind load docker-image ${image_repository}/${image_name}:${image_tag} --name cloudevents-conductor-e2e
elif command -v podman &> /dev/null; then
    podman save ${image_repository}/${image_name}:${image_tag} -o /tmp/cloudevents-conductor.tar
    kind load image-archive /tmp/cloudevents-conductor.tar --name cloudevents-conductor-e2e
    rm /tmp/cloudevents-conductor.tar
else
    echo "Neither Docker nor Podman is installed, exiting"
    exit 1
fi

# 3. deploy maestro
helm install maestro ${CURRENT_DIR}/../../deploy/maestro

# wait until maestro deployment available
kubectl wait --for=condition=available --timeout=120s deployment/maestro-db -n maestro
kubectl wait --for=condition=available --timeout=120s deployment/maestro -n maestro

# patch maestro services to be access externally
kubectl patch service maestro -n maestro \
  -p '{"spec": {"type": "NodePort", "ports": [{"port":8000, "protocol":"TCP", "targetPort":8000, "nodePort":30080}]}}'
kubectl patch service maestro-grpc -n maestro \
  -p '{"spec": {"type": "NodePort", "ports": [{"port":8090, "protocol":"TCP", "targetPort":8090, "nodePort":30090}]}}'

# 4. deploy cluster-manager
go install open-cluster-management.io/clusteradm/cmd/clusteradm@main

clusteradm init --wait --bundle-version=latest

kubectl patch clustermanager cluster-manager --type='json' --patch "$(printf '[
  {
    "op": "add",
    "path": "/spec/registrationConfiguration/registrationDrivers/-",
    "value": {
      "authType": "grpc"
    }
  },
  {
    "op": "add",
    "path": "/spec/serverConfiguration",
    "value": {
      "endpointsExposure": [
        {
          "protocol": "grpc"
        }
      ],
      "imagePullSpec": "%s"
    }
  }
]' "${image_repository}/${image_name}:${image_tag}")"


# wait until clustermanager is applied
kubectl wait --for=condition=applied --timeout=120s clustermanager/cluster-manager

# 5. prepare grpc-server config
db_pw=$(kubectl -n maestro get secret maestro-db-config -o jsonpath='{.data.password}' | base64 -d)
cat << EOF | kubectl -n open-cluster-management-hub create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: grpc-server-config
data:
  config.yaml: |
    grpc_config:
      tls_cert_file: /var/run/secrets/hub/grpc/serving-cert/tls.crt
      tls_key_file: /var/run/secrets/hub/grpc/serving-cert/tls.key
      client_ca_file: /var/run/secrets/hub/grpc/ca/ca-bundle.crt
    db_config:
      host: maestro-db.maestro
      port: 5432
      name: maestro
      username: maestro
      password: ${db_pw}
      sslmode: disable
EOF

# wait until grpc-server deployment is available
kubectl wait --for=condition=available --timeout=120s deployment/cluster-manager-grpc-server -n open-cluster-management-hub

# wait until grpc-server logs contain 8090 port
timeout=120
start=$(date +%s)
while true; do
  if kubectl logs deployment/cluster-manager-grpc-server -n open-cluster-management-hub | grep -q "8090"; then
    break
  fi
  now=$(date +%s)
  if [ $((now - start)) -ge $timeout ]; then
    echo "Timed out waiting for '8090' in grpc server logs"
    exit 1
  fi
  sleep 5
done

echo "Join $managed_cluster_name to cluster"
joincmd=$(clusteradm get token | grep clusteradm)

$(echo ${joincmd} --force-internal-endpoint-lookup --bundle-version=latest --singleton=true --registration-auth=grpc --grpc-server=cluster-manager-grpc-server.open-cluster-management-hub.svc:8090 | sed "s/<cluster_name>/$managed_cluster_name/g")

# wait until cluster is created
kubectl wait --for=create managedcluster/$managed_cluster_name --timeout=120s

echo "Accept join of $managed_cluster_name"
clusteradm accept --clusters ${managed_cluster_name} --wait

kubectl wait --for=condition=ManagedClusterConditionAvailable=true managedcluster/$managed_cluster_name --timeout=120s
