#!/bin/bash

cluster_name="${1:-cluster1}"

kubectl delete managedclusters ${cluster_name}

kubectl patch clustermanager cluster-manager --type='json' -p='[
  {
    "op": "remove",
    "path": "/spec/registrationConfiguration/registrationDrivers/1"
  }
]'

helm uninstall maestro
