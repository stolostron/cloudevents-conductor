package grpc

import (
	"context"
	"fmt"
	"os"

	dbconfig "github.com/openshift-online/maestro/pkg/config"
	maestrodb "github.com/openshift-online/maestro/pkg/db"
	"github.com/openshift-online/maestro/pkg/db/db_session"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/pflag"
	"github.com/stolostron/cloudevents-conductor/pkg/controller"
	"github.com/stolostron/cloudevents-conductor/pkg/controller/mq"
	"github.com/stolostron/cloudevents-conductor/pkg/services"
	"github.com/stolostron/cloudevents-conductor/pkg/services/db"
	"github.com/stolostron/cloudevents-conductor/pkg/services/db/consumer"
	dbevent "github.com/stolostron/cloudevents-conductor/pkg/services/db/event"
	"github.com/stolostron/cloudevents-conductor/pkg/services/db/resource"
	dbstatusevent "github.com/stolostron/cloudevents-conductor/pkg/services/db/statusevent"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
	"open-cluster-management.io/ocm/pkg/server/grpc"
	"open-cluster-management.io/ocm/pkg/server/services/addon"
	"open-cluster-management.io/ocm/pkg/server/services/cluster"
	"open-cluster-management.io/ocm/pkg/server/services/csr"
	"open-cluster-management.io/ocm/pkg/server/services/event"
	"open-cluster-management.io/ocm/pkg/server/services/lease"
	"open-cluster-management.io/ocm/pkg/server/services/work"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authn"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/options"
)

// GRPCServerConfig defines the configuration for the gRPC server.
// It includes the gRPC server options and the database configuration.
// An example of this configuration is like:
/*
```yaml
grpcConfig:
  tls_cert_file: "/path/to/tls.crt"
  tls_key_file: "/path/to/tls.key"
  client_ca_file: "/path/to/ca.crt"
  serverBindPort: "8090"
dbConfig:
  host: "localhost"
  port: "5432"
  name: "foo"
  username: "bar"
  password: "goo"
  sslmode: "disable"
```
*/
type GRPCServerConfig struct {
	GRPCConfig *grpcoptions.GRPCServerOptions `json:"grpc_config,omitempty" yaml:"grpc_config,omitempty"`
	DBConfig   *dbconfig.DatabaseConfig       `json:"db_config,omitempty" yaml:"db_config,omitempty"`
}

// loadGRPCServerConfig loads the gRPC server configuration from the specified file.
func loadGRPCServerConfig(configPath string) (*GRPCServerConfig, error) {
	grpcServerConfigData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	grpcServerConfig := &GRPCServerConfig{
		GRPCConfig: grpcoptions.NewGRPCServerOptions(),
		DBConfig:   dbconfig.NewDatabaseConfig(),
	}
	if err := yaml.Unmarshal(grpcServerConfigData, grpcServerConfig); err != nil {
		return nil, err
	}

	return grpcServerConfig, nil
}

type GRPCServerOptions struct {
	GRPCServerConfigFile string
}

func NewGRPCServerOptions() *GRPCServerOptions {
	return &GRPCServerOptions{}
}

func (o *GRPCServerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.GRPCServerConfigFile, "server-config", o.GRPCServerConfigFile, "Location of the server configuration file.")
}

func (o *GRPCServerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// Load the gRPC server configuration and database configuration
	grpcServerConfig, err := loadGRPCServerConfig(o.GRPCServerConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load gRPC server config: %w", err)
	}

	// Retrieve the gRPC server options and database configuration
	serverOptions := grpcServerConfig.GRPCConfig
	dbConfig := grpcServerConfig.DBConfig

	// Create a session factory for the database connection
	sessionFactory := db_session.NewProdFactory(dbConfig)
	defer func() {
		// ensure the session factory is closed when the context is done
		if err := sessionFactory.Close(); err != nil {
			klog.Errorf("failed to close session factory: %v", err)
		}
	}()

	// Initialize the database service and controller manager
	dbService := db.NewDBWorkService(resource.NewResourceService(sessionFactory),
		dbstatusevent.NewStatusEventService(sessionFactory))
	ctrMgr := controller.NewSpecControllerManager(maestrodb.NewAdvisoryLockFactory(sessionFactory),
		dbevent.NewEventService(sessionFactory))

	// Listen for db events and add them to the controller manager in a goroutine
	go sessionFactory.NewListener(ctx, "events", ctrMgr.AddEvent)

	clients, err := grpc.NewClients(controllerContext)
	if err != nil {
		return err
	}

	workService := work.NewWorkService(clients.WorkClient, clients.WorkInformers.Work().V1().ManifestWorks())

	managedClusterController := controller.NewManagedClusterController(
		clients.ClusterInformers.Cluster().V1().ManagedClusters(),
		controllerContext.EventRecorder,
		mq.NewMessageQueueAuthzCreator(),
		consumer.NewConsumerService(sessionFactory),
	)

	// TODO: start the controller as a prehook of grpc server
	go managedClusterController.Run(ctx, 1)

	return grpcoptions.NewServer(serverOptions).WithPreStartHooks(ctrMgr).WithPreStartHooks(clients).WithAuthenticator(
		grpcauthn.NewTokenAuthenticator(clients.KubeClient),
	).WithAuthenticator(
		grpcauthn.NewMtlsAuthenticator(),
	).WithService(
		clusterce.ManagedClusterEventDataType,
		cluster.NewClusterService(clients.ClusterClient, clients.ClusterInformers.Cluster().V1().ManagedClusters()),
	).WithService(
		csrce.CSREventDataType,
		csr.NewCSRService(clients.KubeClient, clients.KubeInformers.Certificates().V1().CertificateSigningRequests()),
	).WithService(
		addonce.ManagedClusterAddOnEventDataType,
		addon.NewAddonService(clients.AddOnClient, clients.AddOnInformers.Addon().V1alpha1().ManagedClusterAddOns()),
	).WithService(
		eventce.EventEventDataType,
		event.NewEventService(clients.KubeClient),
	).WithService(
		leasece.LeaseEventDataType,
		lease.NewLeaseService(clients.KubeClient, clients.KubeInformers.Coordination().V1().Leases()),
	).WithService(
		payload.ManifestBundleEventDataType,
		services.NewRouterService(dbService,
			ctrMgr,
			workService,
			clients.WorkInformers.Work().V1().ManifestWorks()),
	).Run(ctx)
}
