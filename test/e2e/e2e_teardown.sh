#!/bin/bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
CURRENT_DIR="$(cd ${CURRENT_DIR} && pwd)"

if ! command -v kind >/dev/null 2>&1; then 
    echo "This script will install kind (https://kind.sigs.k8s.io/) on your machine."
    curl -Lo ./kind-amd64 "https://kind.sigs.k8s.io/dl/v0.12.0/kind-$(uname)-amd64"
    chmod +x ./kind-amd64
    sudo mv ./kind-amd64 /usr/local/bin/kind
fi

kind delete cluster --name cloudevents-conductor-e2e

# cleanup the generated files
rm -rf ${CURRENT_DIR}/.kubeconfig
rm -rf ${CURRENT_DIR}/ocm
