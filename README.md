# CloudEvents Conductor
The CloudEvents Conductor is deployed on the hub and exposed to allow connections from managed clusters. It provides the following features:
- **gRPC Server**: Handles incoming requests from managed clusters.
- **Router Service**: Routes requests to the appropriate service based on the source. If the source is `kube`, requests are routed to the Work Service to handle Kubernetes requests. If the source is `maestro`, requests are routed to the DB Service to handle Maestro requests.
- **Consumer Controller**: Maps managed clusters to Maestro consumers and calls the Maestro API to create consumers.

## Overview

The diagram below shows how the CloudEvents Conductor acts as a central hub, coordinating communication between the SQL Database, Kubernetes Resources, and Klusterlet to manage resources across Kubernetes clusters. The CloudEvents Conductor does not communicate directly with the Maestro server; instead, interactions with Maestro are handled via the SQL Database Listen and Notify mechanism.

<p align="center">
    <img src="./overview.png" alt="Cloudevents Conductor" width="70%">
</p>

## Deploy

### Deploy the `cloudevents-conductor` on your hub

1. Run the following command to deploy Maestro on your hub:

```sh
helm install maestro deploy/maestro
```

2. Enable `cloudevents-conductor` on your hub

Run the following command to deploy the `cloudevents-conductor` on your hub:

```sh
deploy/conductor/install.sh
```

### Import your managed cluster

1. Create a `KlusterletConfig` to set the `grpc` type for the `registrationDriver` for cluster registration:

```bash
cat << EOF | oc apply -f -
apiVersion: config.open-cluster-management.io/v1alpha1
kind: KlusterletConfig
metadata:
  name: grpc-config
spec:
  registrationDriver:
    authType: grpc
EOF
```

2. Follow the ACM documentation to import the managed cluster from the ACM console or CLI, and add the annotation `agent.open-cluster-management.io/klusterlet-config=grpc-config` to the `ManagedCluster` resource.

Alternatively, you can add the `grpc` `registrationDriver` `authType` to a global `KlusterletConfig`, and all managed clusters will be imported via `gRPC` by default.
