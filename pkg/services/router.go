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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	"open-cluster-management.io/ocm/pkg/server/services"
	"open-cluster-management.io/ocm/pkg/server/services/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
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

func (s *RouterService) Get(ctx context.Context, resourceID string) (*ce.Event, error) {
	switch {
	case isKubeResource(resourceID):
		id := resourceID[len(services.CloudEventsSourceKube+"::"):]
		return s.workService.Get(ctx, id)
	case isDBResource(resourceID):
		id := resourceID[len(constants.DefaultSourceID+"::"):]
		return s.dbService.Get(ctx, id)
	default:
		return nil, fmt.Errorf("unknown resource ID format: %s", resourceID)
	}
}

// List the cloudEvent from both kube and db service
func (s *RouterService) List(listOpts types.ListOptions) ([]*ce.Event, error) {
	// List the cloudEvents from kube
	evts, err := s.workService.List(listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list work resources: %w", err)
	}

	// List the cloudEvents from db
	dbEvents, err := s.dbService.List(listOpts)
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
	switch {
	case isKubeResource(originalSource):
		// Handle the status update for kube resources
		if err := s.workService.HandleStatusUpdate(ctx, evt); err != nil {
			return fmt.Errorf("failed to handle kube resource status update: %w", err)
		}
	case isDBResource(originalSource):
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
func (w *RouterService) RegisterHandler(handler server.EventHandler) {
	w.specController.Add(&controllers.ControllerConfig{
		Source:   "Resources",
		Handlers: w.ControllerHandlerFuncs(handler),
	})

	// Register the handler for kube resource
	if _, err := w.workInformer.Informer().AddEventHandler(w.EventHandlerFuncs(handler)); err != nil {
		klog.Errorf("failed to register work informer event handler, %v", err)
	}
}

// ControllerHandlerFuncs returns the ControllerHandlerFuncs for the RouterService.
func (w *RouterService) ControllerHandlerFuncs(handler server.EventHandler) map[api.EventType][]controllers.ControllerHandlerFunc {
	return map[api.EventType][]controllers.ControllerHandlerFunc{
		api.CreateEventType: {func(ctx context.Context, resourceID string) error {
			id := generateDBResourceID(constants.DefaultSourceID, resourceID)
			return handler.OnCreate(ctx, payload.ManifestBundleEventDataType, id)
		}},
		api.UpdateEventType: {func(ctx context.Context, resourceID string) error {
			id := generateDBResourceID(constants.DefaultSourceID, resourceID)
			return handler.OnUpdate(ctx, payload.ManifestBundleEventDataType, id)
		}},
		api.DeleteEventType: {func(ctx context.Context, resourceID string) error {
			id := generateDBResourceID(constants.DefaultSourceID, resourceID)
			return handler.OnDelete(ctx, payload.ManifestBundleEventDataType, id)
		}},
	}
}

// EventHandlerFuncs returns the ResourceEventHandlerFuncs for the RouterService.
func (w *RouterService) EventHandlerFuncs(handler server.EventHandler) *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				klog.Errorf("failed to get accessor for work %v", err)
				return
			}
			id := generateKubeResourceID(services.CloudEventsSourceKube, accessor.GetNamespace(), accessor.GetName())
			if err := handler.OnCreate(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			accessor, err := meta.Accessor(newObj)
			if err != nil {
				klog.Errorf("failed to get accessor for work %v", err)
				return
			}
			id := generateKubeResourceID(services.CloudEventsSourceKube, accessor.GetNamespace(), accessor.GetName())
			if err := handler.OnUpdate(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
		DeleteFunc: func(obj interface{}) {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				klog.Errorf("failed to get accessor for work %v", err)
				return
			}
			id := generateKubeResourceID(services.CloudEventsSourceKube, accessor.GetNamespace(), accessor.GetName())
			if err := handler.OnDelete(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
	}
}

func generateKubeResourceID(source, namespace, name string) string {
	// Generate a resource ID based on the source, namespace, and name
	return fmt.Sprintf("%s::%s/%s", source, namespace, name)
}

func generateDBResourceID(source, uuid string) string {
	// Generate a resource ID based on the source and uuid
	return fmt.Sprintf("%s::%s", source, uuid)
}

func isKubeResource(resourceID string) bool {
	if len(resourceID) == 0 {
		return false
	}
	// Check if the resourceID starts with "kube::" or is equal to the kube source indicating it's a kube resource
	return resourceID == services.CloudEventsSourceKube ||
		resourceID[:len(services.CloudEventsSourceKube+"::")] == services.CloudEventsSourceKube+"::"
}

func isDBResource(resourceID string) bool {
	if len(resourceID) == 0 {
		return false
	}
	// Check if the resourceID starts with "maestro::" or is equal to the maestro source indicating it's a DB resource
	return resourceID == constants.DefaultSourceID ||
		resourceID[:len(constants.DefaultSourceID+"::")] == constants.DefaultSourceID+"::"
}
