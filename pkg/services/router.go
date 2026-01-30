package services

import (
	"context"
	"fmt"

	ce "github.com/cloudevents/sdk-go/v2"
	cloudeventstypes "github.com/cloudevents/sdk-go/v2/types"
	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/constants"
	"github.com/openshift-online/maestro/pkg/controllers"
	"github.com/stolostron/cloudevents-conductor/pkg/controller"
	"github.com/stolostron/cloudevents-conductor/pkg/services/db"
	"k8s.io/klog/v2"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	"open-cluster-management.io/ocm/pkg/server/services"
	"open-cluster-management.io/ocm/pkg/server/services/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

var _ server.Service = &RouterService{}

// RouterService implements the server.Service interface for routing the request to dbservice or workservice.
type RouterService struct {
	dbService      *db.DBWorkService
	specController *controller.SpecControllerManager
	workService    *work.WorkService
	workInformer   workinformers.ManifestWorkInformer
}

func NewRouterService(dbService *db.DBWorkService, specController *controller.SpecControllerManager,
	workService *work.WorkService, workInformer workinformers.ManifestWorkInformer) *RouterService {
	return &RouterService{
		dbService:      dbService,
		specController: specController,
		workService:    workService,
		workInformer:   workInformer,
	}
}

// List the cloudEvent from both kube and db service
func (s *RouterService) List(ctx context.Context, listOpts types.ListOptions) ([]*ce.Event, error) {
	// List the cloudEvents from kube
	evts, err := s.workService.List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list work resources: %w", err)
	}

	// List the cloudEvents from db
	dbEvents, err := s.dbService.List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list db resources: %w", err)
	}

	// Combine the events from both kube and db services
	return append(evts, dbEvents...), nil
}

// HandleStatusUpdate processes the resource status update from the agent.
func (s *RouterService) HandleStatusUpdate(ctx context.Context, evt *ce.Event) error {
	if evt == nil {
		return fmt.Errorf("event cannot be nil")
	}
	originalSource, err := cloudeventstypes.ToString(evt.Context.GetExtensions()[types.ExtensionOriginalSource])
	if err != nil {
		return fmt.Errorf("failed to get original source from event: %w", err)
	}
	switch originalSource {
	case services.CloudEventsSourceKube:
		// Handle the status update for kube resources
		if err := s.workService.HandleStatusUpdate(ctx, evt); err != nil {
			return fmt.Errorf("failed to handle kube resource status update: %w", err)
		}
	case constants.DefaultSourceID:
		// Handle the status update for db resources
		if err := s.dbService.HandleStatusUpdate(ctx, evt); err != nil {
			return fmt.Errorf("failed to handle db resource status update: %w", err)
		}
	default:
		return fmt.Errorf("unknown resource original source: %s", originalSource)
	}

	return nil
}

// RegisterHandler registers the event handler for the RouterService.
func (s *RouterService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	s.specController.Add(&controllers.ControllerConfig{
		Source:   "Resources",
		Handlers: s.ControllerHandlerFuncs(handler),
	})

	// Register the handler for kube resource
	if _, err := s.workInformer.Informer().AddEventHandler(s.workService.EventHandlerFuncs(ctx, handler)); err != nil {
		klog.Errorf("failed to register work informer event handler, %v", err)
	}
}

// ControllerHandlerFuncs returns the ControllerHandlerFuncs for the RouterService.
func (s *RouterService) ControllerHandlerFuncs(handler server.EventHandler) map[api.EventType][]controllers.ControllerHandlerFunc {
	return map[api.EventType][]controllers.ControllerHandlerFunc{
		api.CreateEventType: {func(ctx context.Context, resourceID string) error {
			evt, err := s.dbService.Get(ctx, resourceID, types.CreateRequestAction)
			if err != nil {
				return err
			}
			return handler.HandleEvent(ctx, evt)
		}},
		api.UpdateEventType: {func(ctx context.Context, resourceID string) error {
			evt, err := s.dbService.Get(ctx, resourceID, types.UpdateRequestAction)
			if err != nil {
				return err
			}
			return handler.HandleEvent(ctx, evt)
		}},
		api.DeleteEventType: {func(ctx context.Context, resourceID string) error {
			evt, err := s.dbService.Get(ctx, resourceID, types.DeleteRequestAction)
			if err != nil {
				return err
			}
			return handler.HandleEvent(ctx, evt)
		}},
	}
}
