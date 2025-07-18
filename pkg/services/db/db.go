package db

import (
	"context"
	"fmt"

	ce "github.com/cloudevents/sdk-go/v2"
	cetypes "github.com/cloudevents/sdk-go/v2/types"
	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/services"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/common"
	workpayload "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/source/codec"
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
func (s *DBWorkService) Get(ctx context.Context, resourceID string) (*ce.Event, error) {

	resource, err := s.resourceService.Get(ctx, resourceID)
	if err != nil {
		// if the resource is not found, it indicates the resource has been processed.
		if err.Is404() {
			return nil, kubeerrors.NewNotFound(schema.GroupResource{Resource: "manifestbundles"}, resourceID)
		}
		return nil, kubeerrors.NewInternalError(err)
	}

	return encodeResourceSpec(resource)
}

// List the cloudEvent from the service
func (s *DBWorkService) List(listOpts types.ListOptions) ([]*ce.Event, error) {
	resources, err := s.resourceService.List(listOpts)
	if err != nil {
		return nil, err
	}

	evts := []*ce.Event{}
	for _, res := range resources {
		evt, err := encodeResourceSpec(res)
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
	resource, err := decodeResourceStatus(evt)
	if err != nil {
		return fmt.Errorf("failed to decode cloudevent: %v", err)
	}

	// handle the resource status update according status update type
	if err := handleStatusUpdate(ctx, resource, s.resourceService, s.statusEventService); err != nil {
		return fmt.Errorf("failed to handle resource status update %s: %s", resource.ID, err.Error())
	}

	return nil
}

// RegisterHandler register the handler to the service.
func (s *DBWorkService) RegisterHandler(handler server.EventHandler) {
}

// handleStatusUpdate processes the resource status update from the agent.
// The resource argument contains the updated status.
// The function performs the following steps:
// 1. Verifies if the resource is still in the Maestro server and checks if the consumer name matches.
// 2. Retrieves the resource from Maestro and fills back the work metadata from the spec event to the status event.
// 3. Checks if the resource has been deleted from the agent. If so, creates a status event and deletes the resource from Maestro;
// otherwise, updates the resource status and creates a status event.
func handleStatusUpdate(ctx context.Context, resource *api.Resource, resourceService services.ResourceService, statusEventService services.StatusEventService) error {
	klog.Infof("handle resource status update %s by the current instance", resource.ID)

	found, svcErr := resourceService.Get(ctx, resource.ID)
	if svcErr != nil {
		if svcErr.Is404() {
			klog.Warningf("skipping resource %s as it is not found", resource.ID)
			return nil
		}

		return fmt.Errorf("failed to get resource %s, %s", resource.ID, svcErr.Error())
	}

	if found.ConsumerName != resource.ConsumerName {
		return fmt.Errorf("unmatched consumer name %s for resource %s", resource.ConsumerName, resource.ID)
	}

	// set the resource source and type back for broadcast
	resource.Source = found.Source

	// convert the resource status to cloudevent
	statusEvent, err := api.JSONMAPToCloudEvent(resource.Status)
	if err != nil {
		return fmt.Errorf("failed to convert resource status to cloudevent: %v", err)
	}

	// convert the resource spec to cloudevent
	specEvent, err := api.JSONMAPToCloudEvent(found.Payload)
	if err != nil {
		return fmt.Errorf("failed to convert resource spec to cloudevent: %v", err)
	}

	// set work meta from spec event to status event
	if workMeta, ok := specEvent.Extensions()[codec.ExtensionWorkMeta]; ok {
		statusEvent.SetExtension(codec.ExtensionWorkMeta, workMeta)
	}

	// convert the resource status cloudevent back to resource status jsonmap
	resource.Status, err = api.CloudEventToJSONMap(statusEvent)
	if err != nil {
		return fmt.Errorf("failed to convert resource status cloudevent to json: %v", err)
	}

	// decode the cloudevent data as manifest status
	statusPayload := &workpayload.ManifestBundleStatus{}
	if err := statusEvent.DataAs(statusPayload); err != nil {
		return fmt.Errorf("failed to decode cloudevent data as resource status: %v", err)
	}

	// if the resource has been deleted from agent, create status event and delete it from maestro
	if meta.IsStatusConditionTrue(statusPayload.Conditions, common.ResourceDeleted) {
		_, sErr := statusEventService.Create(ctx, &api.StatusEvent{
			ResourceID:      resource.ID,
			ResourceSource:  resource.Source,
			ResourceType:    resource.Type,
			Payload:         found.Payload,
			Status:          resource.Status,
			StatusEventType: api.StatusDeleteEventType,
		})
		if sErr != nil {
			return fmt.Errorf("failed to create status event for resource status delete %s: %s", resource.ID, sErr.Error())
		}
		if svcErr := resourceService.Delete(ctx, resource.ID); svcErr != nil {
			return fmt.Errorf("failed to delete resource %s: %s", resource.ID, svcErr.Error())
		}

		klog.Infof("resource %s status delete event was sent", resource.ID)
	} else {
		// update the resource status
		_, updated, svcErr := resourceService.UpdateStatus(ctx, resource)
		if svcErr != nil {
			return fmt.Errorf("failed to update resource status %s: %s", resource.ID, svcErr.Error())
		}

		// create the status event only when the resource is updated
		if updated {
			_, sErr := statusEventService.Create(ctx, &api.StatusEvent{
				ResourceID:      resource.ID,
				StatusEventType: api.StatusUpdateEventType,
			})
			if sErr != nil {
				return fmt.Errorf("failed to create status event for resource status update %s: %s", resource.ID, sErr.Error())
			}

			klog.Infof("resource %s status update event was sent", resource.ID)
		}
	}

	return nil
}

// decodeResourceStatus translates a CloudEvent into a resource containing the status JSON map.
func decodeResourceStatus(evt *ce.Event) (*api.Resource, error) {
	evtExtensions := evt.Context.GetExtensions()

	clusterName, err := cetypes.ToString(evtExtensions[types.ExtensionClusterName])
	if err != nil {
		return nil, fmt.Errorf("failed to get clustername extension: %v", err)
	}

	resourceID, err := cetypes.ToString(evtExtensions[types.ExtensionResourceID])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceid extension: %v", err)
	}

	resourceVersion, err := cetypes.ToInteger(evtExtensions[types.ExtensionResourceVersion])
	if err != nil {
		return nil, fmt.Errorf("failed to get resourceversion extension: %v", err)
	}

	status, err := api.CloudEventToJSONMap(evt)
	if err != nil {
		return nil, fmt.Errorf("failed to convert cloudevent to resource status: %v", err)
	}

	resource := &api.Resource{
		Source:       evt.Source(),
		ConsumerName: clusterName,
		Version:      resourceVersion,
		Meta: api.Meta{
			ID: resourceID,
		},
		Status: status,
	}

	return resource, nil
}

// encodeResourceSpec translates a resource spec JSON map into a CloudEvent.
func encodeResourceSpec(resource *api.Resource) (*ce.Event, error) {
	evt, err := api.JSONMAPToCloudEvent(resource.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource payload to cloudevent: %v", err)
	}

	eventType := types.CloudEventsType{
		CloudEventsDataType: workpayload.ManifestBundleEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.EventAction("create_request"),
	}
	evt.SetType(eventType.String())
	evt.SetSource("maestro")
	// TODO set resource.Source with a new extension attribute if the agent needs
	evt.SetExtension(types.ExtensionResourceID, resource.ID)
	evt.SetExtension(types.ExtensionResourceVersion, int64(resource.Version))
	evt.SetExtension(types.ExtensionClusterName, resource.ConsumerName)

	if !resource.GetDeletionTimestamp().IsZero() {
		evt.SetExtension(types.ExtensionDeletionTimestamp, resource.GetDeletionTimestamp().Time)
	}

	return evt, nil
}
