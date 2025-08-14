#!/bin/bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
CURRENT_DIR="$(cd ${CURRENT_DIR} && pwd)"

kubectl create ns open-cluster-management-agent

kubectl -n open-cluster-management-agent create secret generic bootstrap-hub-kubeconfig \
    --from-file=kubeconfig=$CURRENT_DIR/config/bootstrap.kubeconfig \
    --from-file=config.yaml=$CURRENT_DIR/config/bootstrap.grpcconfig \
    --from-file=ca.crt=$CURRENT_DIR/config/certs/ca.crt \
    --from-file=client.crt=$CURRENT_DIR/config/certs/client.crt \
    --from-file=client.key=$CURRENT_DIR/config/certs/client.key
