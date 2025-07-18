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
	"open-cluster-management.io/ocm/pkg/server/services"
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
	if isKubeResource(resourceID) {
		return s.workService.Get(ctx, resourceID)
	}
	return s.dbService.Get(ctx, resourceID)
}

// List the cloudEvent from the service
func (s *RouterService) List(listOpts types.ListOptions) ([]*ce.Event, error) {
	if isKubeResource(listOpts.Source) {
		return s.workService.List(listOpts)
	}
	return s.dbService.List(listOpts)
}

// HandleStatusUpdate processes the resource status update from the agent.
func (s *RouterService) HandleStatusUpdate(ctx context.Context, evt *ce.Event) error {
	if isKubeResource(evt.Source()) {
		if err := s.workService.HandleStatusUpdate(ctx, evt); err != nil {
			klog.Errorf("failed to handle status update for work resource %s: %v", evt.Source(), err)
			return fmt.Errorf("failed to handle status update for work resource %s: %w", evt.Source(), err)
		}
	}

	return s.dbService.HandleStatusUpdate(ctx, evt)
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
			id := generateResourceID(services.CloudEventsSourceKube, accessor.GetNamespace(), accessor.GetName())
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
			id := generateResourceID(services.CloudEventsSourceKube, accessor.GetNamespace(), accessor.GetName())
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
			id := generateResourceID(services.CloudEventsSourceKube, accessor.GetNamespace(), accessor.GetName())
			if err := handler.OnDelete(context.Background(), payload.ManifestBundleEventDataType, id); err != nil {
				klog.Error(err)
			}
		},
	}
}

func generateResourceID(source, namespace, name string) string {
	// Generate a resource ID based on the source, namespace, and name
	return fmt.Sprintf("%s::%s/%s", source, namespace, name)
}

func isKubeResource(resourceID string) bool {
	if len(resourceID) == 0 {
		return false
	}
	// Check if the resourceID starts with "kube::" or is equal to the kube source indicating it's a kube resource
	return resourceID == services.CloudEventsSourceKube ||
		resourceID[:len(services.CloudEventsSourceKube+"::")] == services.CloudEventsSourceKube+"::"
}
