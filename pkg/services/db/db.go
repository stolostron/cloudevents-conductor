package db

import (
	"context"
	"fmt"

	ce "github.com/cloudevents/sdk-go/v2"
	maestroserver "github.com/openshift-online/maestro/cmd/maestro/server"
	"github.com/openshift-online/maestro/pkg/services"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/server"
)

var _ server.Service = &DBWorkService{}

// DBWorkService implements the server.Service interface for handling work resources.
type DBWorkService struct {
	resourceService    services.ResourceService
	statusEventService services.StatusEventService
}

func NewDBWorkService(resourceService services.ResourceService,
	statusEventService services.StatusEventService) *DBWorkService {
	return &DBWorkService{
		resourceService:    resourceService,
		statusEventService: statusEventService,
	}
}

// Get the cloudEvent based on resourceID from the service
func (s *DBWorkService) Get(ctx context.Context, resourceID string, action types.EventAction) (*ce.Event, error) {
	resource, err := s.resourceService.Get(ctx, resourceID)
	if err != nil {
		// if the resource is not found, it indicates the resource has been processed.
		if err.Is404() {
			return nil, kubeerrors.NewNotFound(schema.GroupResource{Resource: "manifestbundles"}, resourceID)
		}
		return nil, kubeerrors.NewInternalError(err)
	}

	return maestroserver.EncodeResourceSpec(resource, action)
}

// List the cloudEvent from the service
func (s *DBWorkService) List(ctx context.Context, listOpts types.ListOptions) ([]*ce.Event, error) {
	resources, err := s.resourceService.List(ctx, listOpts)
	if err != nil {
		return nil, err
	}

	evts := []*ce.Event{}
	for _, res := range resources {
		evt, err := maestroserver.EncodeResourceSpec(res, types.ResyncResponseAction)
		if err != nil {
			return nil, kubeerrors.NewInternalError(err)
		}
		evts = append(evts, evt)
	}

	return evts, nil
}

// HandleStatusUpdate processes the resource status update from the agent.
func (s *DBWorkService) HandleStatusUpdate(ctx context.Context, evt *ce.Event) error {
	// decode the cloudevent data as resource with status
	resource, err := maestroserver.DecodeResourceStatus(evt)
	if err != nil {
		return fmt.Errorf("failed to decode cloudevent: %v", err)
	}

	// handle the resource status update according status update type
	if err := maestroserver.HandleStatusUpdate(ctx, resource, s.resourceService, s.statusEventService); err != nil {
		return fmt.Errorf("failed to handle resource status update %s: %s", resource.ID, err.Error())
	}

	return nil
}

// RegisterHandler register the handler to the service.
func (s *DBWorkService) RegisterHandler(ctx context.Context, handler server.EventHandler) {
	// do nothing, use router to handle event
}
