# CloudEvents Conductor
The CloudEvents Conductor is deployed on the hub and exposed to allow connections from managed clusters. It should provide the following features:
- **gRPC Server**: The gRPC server is responsible for handling incoming requests from managed clusters.
- **Router Service**: Routes the requests to the appropriate service based on the source. If the source is kube, then route requests to the Work Service to handle Kube requests. If the source is maestro, then route requests to the DB Service to handle Maestro requests.
- **Consumer Controller**: Maps the managed cluster to the Maestro consumer and calls the Maestro API to create the consumer.

## Overview

The diagram shows how the Cloudevents Conductor acts as a central place, coordinating communication between the SQL Database, Kube Resources, and Klusterlet to manage resources across Kubernetes clusters. The Cloudevents Conductor does not communicate directly with the Maestro server; instead, interactions with Maestro are handled via the SQL Database Listen and Notify mechanism.

<p align="center">
    <img src="./overview.png" alt="Cloudevents Conductor" width="70%">
</p>

## Deploy

### Deploy the cloudevents-conductor on your hub

1. Run following command to deploy Maestro on your hub

```sh
helm install maestro .deploy/maestro
```

2. Run following command to deploy the `cloudevents-conductor` on your hub

```sh
deploy/conductor/install.sh
```

### Import your managed cluster

1. Prepare the bootstrap configs from your hub

```sh
deploy/managedcluster/hub.sh <your-managedcluster-name>
```

2. Create bootstrap secret on your managedcluster

```sh
deploy/managedcluster/spoke.sh
```

3. Install klusterlet on your managedcluster

```sh
helm repo add ocm https://open-cluster-management.io/helm-charts
helm repo update
helm search repo ocm

helm install klusterlet ocm/klusterlet \
    --set klusterlet.clusterName=<your-managedcluster-name> \
    --set klusterlet.registrationConfiguration.registrationDriver.authType=grpc \
    --namespace=open-cluster-management \
    --create-namespace
```

4. Accept you managedcluster on your hub

```sh
kubectl patch managedcluster <your-managedcluster-name> -p='{\"spec\":{\"hubAcceptsClient\":true}}' --type=merge
kubectl get csr -l open-cluster-management.io/cluster-name=<your-managedcluster-name> | grep Pending | awk '{print \$1}' | xargs kubectl certificate approve
```