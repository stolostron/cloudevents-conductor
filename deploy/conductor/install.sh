#!/bin/bash

set -o errexit

cloudevents_conductor_image="${1:-quay.io/redhat-user-workloads/crt-redhat-acm-tenant/cloudevents-conductor-main@sha256:3d61937d26c97e49570aaadfbc43296dbfb24fa108bdfece5899d3ca3925121e}"

echo "Prepare the cloudevents-conductor configuration"
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

echo "Create the route for the cloudevents-conductor"
cat << EOF | kubectl -n open-cluster-management-hub create -f -
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: grpc-server
spec:
  to:
    kind: Service
    name: cluster-manager-grpc-server
  port:
    targetPort: 8090
  tls:
    termination: passthrough
    insecureEdgeTerminationPolicy: None
EOF

echo "Patch the ClusterManager to enable cloudevents-conductor"
host=$(kubectl -n open-cluster-management-hub get route grpc-server -o jsonpath='{.spec.host}')

# Check if registrationConfiguration exists
if kubectl get clustermanager cluster-manager -o jsonpath='{.spec.registrationConfiguration}' | grep -q .; then
  # registrationConfiguration exists, add to registrationDrivers array
  kubectl patch clustermanager cluster-manager --type='json' --patch "$(printf '[
    {
      "op": "add",
      "path": "/spec/registrationConfiguration/registrationDrivers/-",
      "value": {
        "authType": "grpc",
        "grpc": {
          "imagePullSpec": "%s",
          "endpointExposure": {
            "type": "hostname",
            "hostname": {
              "value": "%s"
            }
          }
        }
      }
    }
  ]' "$cloudevents_conductor_image" "$host")"
else
  # registrationConfiguration doesn't exist, create it
  kubectl patch clustermanager cluster-manager --type='json' --patch "$(printf '[
    {
      "op": "add",
      "path": "/spec/registrationConfiguration",
      "value": {
        "registrationDrivers": [
          {
            "authType": "grpc",
            "grpc": {
              "imagePullSpec": "%s",
              "endpointExposure": {
                "type": "hostname",
                "hostname": {
                  "value": "%s"
                }
              }
            }
          }
        ]
      }
    }
  ]' "$cloudevents_conductor_image" "$host")"
fi
