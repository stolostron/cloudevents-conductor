#!/bin/bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
CURRENT_DIR="$(cd ${CURRENT_DIR} && pwd)"

cluster_name="${1:-cluster1}"

echo "Prepare bootstrap config for $cluster_name"

# prepare certs
rm -rf $CURRENT_DIR/config
mkdir -p $CURRENT_DIR/config/certs

# prepare kube bootstrap config
kubectl config view --flatten --minify > $CURRENT_DIR/config/bootstrap.kubeconfig

# prepare grpc bootstrap certs
kubectl -n open-cluster-management-hub get cm ca-bundle-configmap -ojsonpath='{.data.ca-bundle\.crt}' > $CURRENT_DIR/config/certs/ca.crt
kubectl -n open-cluster-management-hub get secrets signer-secret -ojsonpath="{.data.tls\.crt}" | base64 -d > $CURRENT_DIR/config/certs/signer.crt
kubectl -n open-cluster-management-hub get secrets signer-secret -ojsonpath="{.data.tls\.key}" | base64 -d > $CURRENT_DIR/config/certs/signer.key
cat << EOF > $CURRENT_DIR/config/ext.conf
authorityKeyIdentifier=keyid,issuer:always
basicConstraints=CA:FALSE
keyUsage=keyEncipherment,dataEncipherment,digitalSignature
extendedKeyUsage=clientAuth
EOF
openssl genpkey -algorithm RSA -out $CURRENT_DIR/config/certs/client.key -pkeyopt rsa_keygen_bits:2048
openssl req -new -key $CURRENT_DIR/config/certs/client.key -out $CURRENT_DIR/config/certs/client.csr -subj "/CN=${cluster_name}-grpc-client"
openssl x509 -req \
  -in $CURRENT_DIR/config/certs/client.csr \
  -CA $CURRENT_DIR/config/certs/signer.crt \
  -CAkey $CURRENT_DIR/config/certs/signer.key \
  -CAcreateserial \
  -out $CURRENT_DIR/config/certs/client.crt \
  -days 1 \
  -sha256 \
  -extfile $CURRENT_DIR/config/ext.conf

# prepare grpc bootstrap config
host=$(kubectl -n open-cluster-management-hub get route grpc-server -o jsonpath='{.spec.host}')
cat << EOF > $CURRENT_DIR/config/bootstrap.grpcconfig
url: ${host}
caFile: /spoke/bootstrap/ca.crt
clientCertFile: /spoke/bootstrap/client.crt
clientKeyFile: /spoke/bootstrap/client.key
EOF

# prepare the bootstrap clusterrole/clusterrolebinding
cat << EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: system:open-cluster-management:grpc-bootstrap
rules:
- apiGroups: ["", "events.k8s.io"]
  resources: ["events"]
  verbs: ["list", "watch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["list", "watch"]
- apiGroups: ["addon.open-cluster-management.io"]
  resources: ["managedclusteraddons"]
  verbs: ["list", "watch"]
- apiGroups: ["certificates.k8s.io"]
  resources: ["certificatesigningrequests"]
  verbs: ["create", "get", "list", "watch"]
- apiGroups: ["cluster.open-cluster-management.io"]
  resources: ["managedclusters"]
  verbs: ["create", "get", "list", "watch"] 

---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: system:open-cluster-management:grpc-bootstrap
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: system:open-cluster-management:grpc-bootstrap
subjects:
  - kind: User
    name: ${cluster_name}-grpc-client
EOF
