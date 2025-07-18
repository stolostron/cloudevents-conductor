# CloudEvents Conductor
The CloudEvents Conductor is deployed on the hub and exposed to allow connections from managed clusters. It should provide the following features:
- **gRPC Server**: The gRPC server is responsible for handling incoming requests from managed clusters.
- **DB Service**: Leverages the Postgres listen/notify mechanism to handle status update events.
- **Consumer Controller**: Maps the managed cluster to the Maestro consumer and calls the Maestro API to create the consumer.