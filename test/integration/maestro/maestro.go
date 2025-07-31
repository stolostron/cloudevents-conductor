package maestro

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/openshift-online/maestro/cmd/maestro/environments"
	envtypes "github.com/openshift-online/maestro/cmd/maestro/environments/types"
	"github.com/openshift-online/maestro/cmd/maestro/server"
	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/controllers"
	"github.com/openshift-online/maestro/pkg/dao"
	"github.com/openshift-online/maestro/pkg/db"
	"github.com/openshift-online/maestro/pkg/db/db_session"
	"github.com/openshift-online/maestro/pkg/event"
	"github.com/openshift-online/maestro/pkg/services"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

var once sync.Once

type Maestro struct {
	dbFactory         db.SessionFactory
	eventBroadcaster  *event.EventBroadcaster
	apiServer         server.Server
	controllerManager *server.ControllersServer
	eventServer       server.EventServer
	resources         services.ResourceService
	instanceDao       dao.InstanceDao
}

func NewMaestro(dbPort uint32) *Maestro {
	var m *Maestro
	once.Do(func() {
		env := environments.Environment()
		env.Name = envtypes.TestingEnv
		if err := env.AddFlags(pflag.CommandLine); err != nil {
			klog.Fatalf("Unable to add environment flags: %s", err.Error())
		}
		if logLevel := os.Getenv("LOGLEVEL"); logLevel != "" {
			klog.Infof("Using custom loglevel: %s", logLevel)
			pflag.CommandLine.Set("-v", logLevel)
		}
		pflag.Parse()

		// set database configuration
		env.Config.Database.Host = "localhost"
		env.Config.Database.Port = int(dbPort)
		env.Config.Database.Name = "maestro"
		env.Config.Database.Username = "postgres"
		env.Config.Database.Password = "postgres"

		// create database session
		env.Database.SessionFactory = db_session.NewTestFactory(env.Config.Database)

		// load services
		env.LoadServices()

		// disable gRPC server, JWT, authz, and message broker for testing
		env.Config.GRPCServer.EnableGRPCServer = false
		env.Config.HTTPServer.EnableJWT = false
		env.Config.HTTPServer.EnableAuthz = false
		env.Config.MessageBroker.Disable = true

		eventBroadcaster := event.NewEventBroadcaster()
		m = &Maestro{
			dbFactory:        env.Database.SessionFactory,
			eventBroadcaster: eventBroadcaster,
			apiServer:        server.NewAPIServer(eventBroadcaster),
			eventServer:      server.NewGRPCBroker(eventBroadcaster),
			controllerManager: &server.ControllersServer{
				StatusController: controllers.NewStatusController(
					env.Services.StatusEvents(),
					dao.NewInstanceDao(&env.Database.SessionFactory),
					dao.NewEventInstanceDao(&env.Database.SessionFactory),
				),
			},
			resources:   env.Services.Resources(),
			instanceDao: dao.NewInstanceDao(&env.Database.SessionFactory),
		}
	})

	return m
}

func (m *Maestro) Start(ctx context.Context) error {
	if err := m.MigrateDB(ctx); err != nil {
		return fmt.Errorf("unable to migrate database: %w", err)
	}

	m.startEventBroadcaster(ctx)
	m.startAPIServer(ctx)
	m.startEventServer(ctx)
	m.startControllerManager(ctx)

	// Initialize maestro instance
	if _, err := m.instanceDao.Create(ctx, &api.ServerInstance{
		Meta: api.Meta{
			ID: "maestro",
		},
		Ready: true,
	}); err != nil {
		return fmt.Errorf("unable to create maestro instance: %w", err)
	}

	return nil
}

func (m *Maestro) startEventBroadcaster(ctx context.Context) {
	go func() {
		klog.Info("starting event broadcaster")
		m.eventBroadcaster.Start(ctx)
		klog.Info("event broadcaster stopped")
	}()
}

func (m *Maestro) startAPIServer(ctx context.Context) {
	go func() {
		klog.Info("starting api server")
		m.apiServer.Start()
	}()

	go func() {
		<-ctx.Done()
		if err := m.apiServer.Stop(); err != nil {
			klog.Errorf("unable to stop api server: %s", err.Error())
		}
		klog.Info("api server stopped")
	}()
}

func (m *Maestro) startEventServer(ctx context.Context) {
	go func() {
		klog.Info("starting event server")
		m.eventServer.Start(ctx)
		klog.Info("event server stopped")
	}()
}

func (m *Maestro) startControllerManager(ctx context.Context) {
	m.controllerManager.StatusController.Add(map[api.StatusEventType][]controllers.StatusHandlerFunc{
		api.StatusUpdateEventType: {m.eventServer.OnStatusUpdate},
		api.StatusDeleteEventType: {m.eventServer.OnStatusUpdate},
	})

	go func() {
		// start controller manager
		klog.Info("starting controller manager")
		m.controllerManager.Start(ctx)
		klog.Info("controller manager stopped")
	}()
}

func (m *Maestro) MigrateDB(ctx context.Context) error {
	return db.Migrate(m.dbFactory.New(ctx))
}

func (m *Maestro) CleanDB(ctx context.Context) error {
	g2 := m.dbFactory.New(ctx)

	for _, table := range []string{
		"events",
		"status_events",
		"resources",
		"consumers",
		"server_instances",
	} {
		if g2.Migrator().HasTable(table) {
			// remove table contents instead of dropping table
			sql := fmt.Sprintf("DELETE FROM %s", table)
			if err := g2.Exec(sql).Error; err != nil {
				klog.Errorf("error delete content of table %s: %v", table, err)
				return err
			}
		}
	}
	return nil
}

func (m *Maestro) ResetDB(ctx context.Context) error {
	return m.CleanDB(ctx)
}

func (m *Maestro) ResourceService() services.ResourceService {
	return m.resources
}
