package services

import (
	"context"
	"fmt"

	ce "github.com/cloudevents/sdk-go/v2"
	"github.com/stolostron/cloudevents-conductor/pkg/services/db"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	"open-cluster-management.io/ocm/pkg/server/services/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

var _ server.Service = &RouterService{}

// RouterService implements the server.Service interface for routing the request to dbservice or workservice.
type RouterService struct {
	dbService    *db.DBWorkService
	workService  *work.WorkService
	workInformer workinformers.ManifestWorkInformer
}

func NewRouterService(dbService *db.DBWorkService, workService *work.WorkService,
	workInformer workinformers.ManifestWorkInformer) *RouterService {
	return &RouterService{
		dbService:    dbService,
		workService:  workService,
		workInformer: workInformer,
	}
}

func (s *RouterService) Get(ctx context.Context, resourceID string) (*ce.Event, error) {
	if !isMaestroResource(resourceID) {
		return s.dbService.Get(ctx, resourceID)
	}

	return s.workService.Get(ctx, resourceID)
}

// List the cloudEvent from the service
func (s *RouterService) List(listOpts types.ListOptions) ([]*ce.Event, error) {
	if !isMaestroResource(listOpts.Source) {
		return s.dbService.List(listOpts)
	}
	return s.workService.List(listOpts)
}

// HandleStatusUpdate processes the resource status update from the agent.
func (s *RouterService) HandleStatusUpdate(ctx context.Context, evt *ce.Event) error {
	if !isMaestroResource(evt.Source()) {
		return s.dbService.HandleStatusUpdate(ctx, evt)
	}
	if err := s.workService.HandleStatusUpdate(ctx, evt); err != nil {
		klog.Errorf("failed to handle status update for work resource %s: %v", evt.Source(), err)
		return fmt.Errorf("failed to handle status update for work resource %s: %w", evt.Source(), err)
	}

	return nil
}

// RegisterHandler registers the event handler for the RouterService.
func (w *RouterService) RegisterHandler(handler server.EventHandler) {
	// TODO: Register the handler for db resource

	// Register the handler for kube resource
	if _, err := w.workInformer.Informer().AddEventHandler(w.EventHandlerFuncs(handler)); err != nil {
		klog.Errorf("failed to register work informer event handler, %v", err)
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
			id := accessor.GetNamespace() + "/" + accessor.GetName() + "::maestro"
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
			id := accessor.GetNamespace() + "/" + accessor.GetName() + "::maestro"
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
			id := accessor.GetNamespace() + "/" + accessor.GetName() + "::maestro"
			if err := handler.OnDelete(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
	}
}

func isMaestroResource(resourceID string) bool {
	// Check if the resourceID contains "::maestro" indicating it's a Maestro resource
	return len(resourceID) > 0 && (resourceID[len(resourceID)-len("::maestro"):] == "::maestro" || resourceID == "maestro")
}
