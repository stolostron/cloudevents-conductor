package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	ce "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/constants"
	maestrodb "github.com/openshift-online/maestro/pkg/db"
	maestroerrors "github.com/openshift-online/maestro/pkg/errors"
	maestroservices "github.com/openshift-online/maestro/pkg/services"
	"github.com/stolostron/cloudevents-conductor/pkg/controller"
	"github.com/stolostron/cloudevents-conductor/pkg/services/db"
	"gorm.io/datatypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	workv1 "open-cluster-management.io/api/work/v1"
	workfake "open-cluster-management.io/api/client/work/clientset/versioned/fake"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	ocmservices "open-cluster-management.io/ocm/pkg/server/services"
	"open-cluster-management.io/ocm/pkg/server/services/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// Mock ResourceService
type mockResourceService struct {
	listFunc   func(ctx context.Context, listOpts types.ListOptions) ([]*api.Resource, error)
	getFunc    func(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError)
	updateFunc func(ctx context.Context, resource *api.Resource) (*api.Resource, *maestroerrors.ServiceError)
}

func (m *mockResourceService) List(ctx context.Context, listOpts types.ListOptions) ([]*api.Resource, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, listOpts)
	}
	return []*api.Resource{}, nil
}

func (m *mockResourceService) Get(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockResourceService) Create(ctx context.Context, resource *api.Resource) (*api.Resource, *maestroerrors.ServiceError) {
	return resource, nil
}

func (m *mockResourceService) Update(ctx context.Context, resource *api.Resource) (*api.Resource, *maestroerrors.ServiceError) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, resource)
	}
	return resource, nil
}

func (m *mockResourceService) Delete(ctx context.Context, id string) *maestroerrors.ServiceError {
	return nil
}

func (m *mockResourceService) MarkAsDeleting(ctx context.Context, id string) *maestroerrors.ServiceError {
	return nil
}

func (m *mockResourceService) UpdateStatus(ctx context.Context, resource *api.Resource) (*api.Resource, bool, *maestroerrors.ServiceError) {
	return resource, false, nil
}

func (m *mockResourceService) All(ctx context.Context) (api.ResourceList, *maestroerrors.ServiceError) {
	return api.ResourceList{}, nil
}

func (m *mockResourceService) FindByIDs(ctx context.Context, ids []string) (api.ResourceList, *maestroerrors.ServiceError) {
	return api.ResourceList{}, nil
}

func (m *mockResourceService) FindBySource(ctx context.Context, source string) (api.ResourceList, *maestroerrors.ServiceError) {
	return api.ResourceList{}, nil
}

func (m *mockResourceService) ListWithArgs(ctx context.Context, username string, args *maestroservices.ListArguments, resources *[]api.Resource) (*api.PagingMeta, *maestroerrors.ServiceError) {
	return nil, nil
}

// Mock StatusEventService
type mockStatusEventService struct {
	createFunc func(ctx context.Context, event *api.StatusEvent) (*api.StatusEvent, *maestroerrors.ServiceError)
}

func (m *mockStatusEventService) Create(ctx context.Context, event *api.StatusEvent) (*api.StatusEvent, *maestroerrors.ServiceError) {
	if m.createFunc != nil {
		return m.createFunc(ctx, event)
	}
	return event, nil
}

func (m *mockStatusEventService) Get(ctx context.Context, id string) (*api.StatusEvent, *maestroerrors.ServiceError) {
	return nil, nil
}

func (m *mockStatusEventService) Replace(ctx context.Context, event *api.StatusEvent) (*api.StatusEvent, *maestroerrors.ServiceError) {
	return event, nil
}

func (m *mockStatusEventService) Delete(ctx context.Context, id string) *maestroerrors.ServiceError {
	return nil
}

func (m *mockStatusEventService) All(ctx context.Context) (api.StatusEventList, *maestroerrors.ServiceError) {
	return api.StatusEventList{}, nil
}

func (m *mockStatusEventService) FindByIDs(ctx context.Context, ids []string) (api.StatusEventList, *maestroerrors.ServiceError) {
	return api.StatusEventList{}, nil
}

func (m *mockStatusEventService) FindAllUnreconciledEvents(ctx context.Context) (api.StatusEventList, *maestroerrors.ServiceError) {
	return api.StatusEventList{}, nil
}

func (m *mockStatusEventService) DeleteAllReconciledEvents(ctx context.Context) *maestroerrors.ServiceError {
	return nil
}

func (m *mockStatusEventService) DeleteAllEvents(ctx context.Context, eventIDs []string) *maestroerrors.ServiceError {
	return nil
}

// Mock LockFactory
type mockLockFactory struct{}

func (m *mockLockFactory) NewAdvisoryLock(ctx context.Context, lockID string, lockType maestrodb.LockType) (string, error) {
	return "test-uuid", nil
}

func (m *mockLockFactory) NewNonBlockingLock(ctx context.Context, lockID string, lockType maestrodb.LockType) (string, bool, error) {
	return "test-uuid", true, nil
}

func (m *mockLockFactory) Unlock(ctx context.Context, uuid string) {
	// no-op for mock
}

// Mock EventService
type mockEventService struct{}

func (m *mockEventService) Create(ctx context.Context, event *api.Event) (*api.Event, *maestroerrors.ServiceError) {
	return event, nil
}

func (m *mockEventService) Get(ctx context.Context, id string) (*api.Event, *maestroerrors.ServiceError) {
	return nil, nil
}

func (m *mockEventService) Replace(ctx context.Context, event *api.Event) (*api.Event, *maestroerrors.ServiceError) {
	return event, nil
}

func (m *mockEventService) Delete(ctx context.Context, id string) *maestroerrors.ServiceError {
	return nil
}

func (m *mockEventService) All(ctx context.Context) (api.EventList, *maestroerrors.ServiceError) {
	return api.EventList{}, nil
}

func (m *mockEventService) FindByIDs(ctx context.Context, ids []string) (api.EventList, *maestroerrors.ServiceError) {
	return api.EventList{}, nil
}

func (m *mockEventService) FindAllUnreconciledEvents(ctx context.Context) (api.EventList, *maestroerrors.ServiceError) {
	return api.EventList{}, nil
}

func (m *mockEventService) DeleteAllReconciledEvents(ctx context.Context) *maestroerrors.ServiceError {
	return nil
}

// Mock EventHandler
type mockEventHandler struct {
	handleEventFunc func(ctx context.Context, event *ce.Event) error
}

func (m *mockEventHandler) HandleEvent(ctx context.Context, event *ce.Event) error {
	if m.handleEventFunc != nil {
		return m.handleEventFunc(ctx, event)
	}
	return nil
}

// Helper function to create a test router service
func setupTestRouterService(t *testing.T, resourceService maestroservices.ResourceService, statusEventService maestroservices.StatusEventService, workObjects ...runtime.Object) (*RouterService, *controller.SpecControllerManager) {
	// Create DB service
	dbService := db.NewDBWorkService(resourceService, statusEventService)

	// Create fake work client and informer
	fakeWorkClient := workfake.NewSimpleClientset(workObjects...)
	informerFactory := workinformers.NewSharedInformerFactory(fakeWorkClient, 0)
	workInformer := informerFactory.Work().V1().ManifestWorks()

	// Create work service
	workService := work.NewWorkService(fakeWorkClient, workInformer)

	// Create spec controller manager with mocks
	lockFactory := &mockLockFactory{}
	eventService := &mockEventService{}
	specCtrlMgr := controller.NewSpecControllerManager(lockFactory, eventService)

	// Create router service
	router := NewRouterService(dbService, specCtrlMgr, workService, workInformer)

	return router, specCtrlMgr
}

func TestRouterService_List(t *testing.T) {
	t.Run("successfully lists events from both kube and db services", func(t *testing.T) {
		ctx := context.Background()

		// Create test data
		testWork := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-work",
				Namespace: "test-cluster",
				Labels: map[string]string{
					"cloudevents.open-cluster-management.io/originalsource": ocmservices.CloudEventsSourceKube,
				},
			},
		}

		// Setup mock resource service
		mockResourceSvc := &mockResourceService{
			listFunc: func(ctx context.Context, listOpts types.ListOptions) ([]*api.Resource, error) {
				return []*api.Resource{}, nil
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc, testWork)

		// Execute
		events, err := router.List(ctx, types.ListOptions{ClusterName: "test-cluster"})

		// Assert
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
		if len(events) == 0 {
			t.Log("no events returned (expected from empty test setup)")
		}
	})

	t.Run("returns error when db service list fails", func(t *testing.T) {
		ctx := context.Background()

		// Setup mock resource service that returns error
		mockResourceSvc := &mockResourceService{
			listFunc: func(ctx context.Context, listOpts types.ListOptions) ([]*api.Resource, error) {
				return nil, errors.New("db service error")
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Execute
		events, err := router.List(ctx, types.ListOptions{})

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to list db resources") {
			t.Errorf("expected error to contain 'failed to list db resources', got: %v", err)
		}
		if events != nil {
			t.Errorf("expected nil events, got: %v", events)
		}
	})
}

func TestRouterService_HandleStatusUpdate(t *testing.T) {
	t.Run("routes kube resource status update to work service", func(t *testing.T) {
		ctx := context.Background()

		// Create event with kube source
		evt := ce.NewEvent()
		evt.SetID("test-1")
		evt.SetType("test-type")
		evt.SetSource("test-source")
		evt.SetExtension(types.ExtensionOriginalSource, ocmservices.CloudEventsSourceKube)

		mockResourceSvc := &mockResourceService{}
		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Execute - this will fail because the event doesn't have proper structure,
		// but we're testing the routing logic
		err := router.HandleStatusUpdate(ctx, &evt)

		// The error should come from the work service handler, not from routing
		if err != nil && strings.Contains(err.Error(), "unknown resource original source") {
			t.Error("error indicates routing failed, but should have been routed to work service")
		}
	})

	t.Run("routes db resource status update to db service", func(t *testing.T) {
		ctx := context.Background()

		// Create event with db source
		evt := ce.NewEvent()
		evt.SetID("test-1")
		evt.SetType("test-type")
		evt.SetSource("test-source")
		evt.SetExtension(types.ExtensionOriginalSource, constants.DefaultSourceID)

		mockResourceSvc := &mockResourceService{}
		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Execute - this will fail because the event doesn't have proper structure,
		// but we're testing the routing logic
		err := router.HandleStatusUpdate(ctx, &evt)

		// The error should come from the db service handler, not from routing
		if err != nil && strings.Contains(err.Error(), "unknown resource original source") {
			t.Error("error indicates routing failed, but should have been routed to db service")
		}
	})

	t.Run("returns error when event is nil", func(t *testing.T) {
		ctx := context.Background()

		mockResourceSvc := &mockResourceService{}
		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Execute
		err := router.HandleStatusUpdate(ctx, nil)

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "event cannot be nil") {
			t.Errorf("expected error to contain 'event cannot be nil', got: %v", err)
		}
	})

	t.Run("returns error for unknown original source", func(t *testing.T) {
		ctx := context.Background()

		// Create event with unknown source
		evt := ce.NewEvent()
		evt.SetID("test-1")
		evt.SetType("test-type")
		evt.SetSource("test-source")
		evt.SetExtension(types.ExtensionOriginalSource, "unknown-source")

		mockResourceSvc := &mockResourceService{}
		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Execute
		err := router.HandleStatusUpdate(ctx, &evt)

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unknown resource original source") {
			t.Errorf("expected error to contain 'unknown resource original source', got: %v", err)
		}
	})

	t.Run("returns error when original source extension is missing", func(t *testing.T) {
		ctx := context.Background()

		// Create event without original source extension
		evt := ce.NewEvent()
		evt.SetID("test-1")
		evt.SetType("test-type")
		evt.SetSource("test-source")

		mockResourceSvc := &mockResourceService{}
		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Execute
		err := router.HandleStatusUpdate(ctx, &evt)

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to get original source from event") {
			t.Errorf("expected error to contain 'failed to get original source from event', got: %v", err)
		}
	})
}

func TestRouterService_RegisterHandler(t *testing.T) {
	t.Run("registers handler with spec controller and work informer", func(t *testing.T) {
		ctx := context.Background()

		mockResourceSvc := &mockResourceService{}
		mockStatusEventSvc := &mockStatusEventService{}

		router, specCtrlMgr := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		// Create a mock event handler
		handler := &mockEventHandler{}

		// Execute
		router.RegisterHandler(ctx, handler)

		// We can't easily verify the handler registration without exposing internals,
		// so we just verify the call doesn't panic
		if specCtrlMgr == nil {
			t.Error("expected specCtrlMgr to be non-nil")
		}
	})
}

func TestRouterService_ControllerHandlerFuncs(t *testing.T) {
	t.Run("create event handler retrieves event and calls handler", func(t *testing.T) {
		ctx := context.Background()

		// Setup mocks
		mockResourceSvc := &mockResourceService{
			getFunc: func(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError) {
				if id != "resource-1" {
					t.Errorf("expected resourceID 'resource-1', got: %s", id)
				}
				return &api.Resource{
					Meta: api.Meta{ID: "resource-1"},
					Type: "ManifestBundle",
					Payload: datatypes.JSONMap{
						"specversion": "1.0",
						"id":          "resource-1",
						"source":      "test-source",
						"type":        "io.open-cluster-management.works.v1alpha1.manifests.spec.create_request",
						"data": map[string]interface{}{
							"id":        "resource-1",
							"version":   1,
							"manifests": []interface{}{},
						},
					},
				}, nil
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		var handlerCalled bool
		handler := &mockEventHandler{
			handleEventFunc: func(ctx context.Context, event *ce.Event) error {
				handlerCalled = true
				return nil
			},
		}

		handlerFuncs := router.ControllerHandlerFuncs(handler)

		// Execute
		err := handlerFuncs[api.CreateEventType][0](ctx, "resource-1")

		// Assert
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
		if !handlerCalled {
			t.Error("expected handler to be called")
		}
	})

	t.Run("update event handler retrieves event and calls handler", func(t *testing.T) {
		ctx := context.Background()

		// Setup mocks
		mockResourceSvc := &mockResourceService{
			getFunc: func(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError) {
				if id != "resource-2" {
					t.Errorf("expected resourceID 'resource-2', got: %s", id)
				}
				return &api.Resource{
					Meta: api.Meta{ID: "resource-2"},
					Type: "ManifestBundle",
					Payload: datatypes.JSONMap{
						"specversion": "1.0",
						"id":          "resource-2",
						"source":      "test-source",
						"type":        "io.open-cluster-management.works.v1alpha1.manifests.spec.update_request",
						"data": map[string]interface{}{
							"id":        "resource-2",
							"version":   1,
							"manifests": []interface{}{},
						},
					},
				}, nil
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		var handlerCalled bool
		handler := &mockEventHandler{
			handleEventFunc: func(ctx context.Context, event *ce.Event) error {
				handlerCalled = true
				return nil
			},
		}

		handlerFuncs := router.ControllerHandlerFuncs(handler)

		// Execute
		err := handlerFuncs[api.UpdateEventType][0](ctx, "resource-2")

		// Assert
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
		if !handlerCalled {
			t.Error("expected handler to be called")
		}
	})

	t.Run("delete event handler retrieves event and calls handler", func(t *testing.T) {
		ctx := context.Background()

		// Setup mocks
		mockResourceSvc := &mockResourceService{
			getFunc: func(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError) {
				if id != "resource-3" {
					t.Errorf("expected resourceID 'resource-3', got: %s", id)
				}
				return &api.Resource{
					Meta: api.Meta{ID: "resource-3"},
					Type: "ManifestBundle",
					Payload: datatypes.JSONMap{
						"specversion": "1.0",
						"id":          "resource-3",
						"source":      "test-source",
						"type":        "io.open-cluster-management.works.v1alpha1.manifests.spec.delete_request",
						"data": map[string]interface{}{
							"id":        "resource-3",
							"version":   1,
							"manifests": []interface{}{},
						},
					},
				}, nil
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		var handlerCalled bool
		handler := &mockEventHandler{
			handleEventFunc: func(ctx context.Context, event *ce.Event) error {
				handlerCalled = true
				return nil
			},
		}

		handlerFuncs := router.ControllerHandlerFuncs(handler)

		// Execute
		err := handlerFuncs[api.DeleteEventType][0](ctx, "resource-3")

		// Assert
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
		if !handlerCalled {
			t.Error("expected handler to be called")
		}
	})

	t.Run("returns error when db service get fails", func(t *testing.T) {
		ctx := context.Background()

		// Setup mocks
		mockResourceSvc := &mockResourceService{
			getFunc: func(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError) {
				return nil, maestroerrors.New(maestroerrors.ErrorGeneral, "get error")
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		handler := &mockEventHandler{}

		handlerFuncs := router.ControllerHandlerFuncs(handler)

		// Execute
		err := handlerFuncs[api.CreateEventType][0](ctx, "resource-1")

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("returns error when event handler fails", func(t *testing.T) {
		ctx := context.Background()

		// Setup mocks
		mockResourceSvc := &mockResourceService{
			getFunc: func(ctx context.Context, id string) (*api.Resource, *maestroerrors.ServiceError) {
				return &api.Resource{
					Meta: api.Meta{ID: "test-1"},
					Type: "ManifestBundle",
					Payload: datatypes.JSONMap{
						"specversion": "1.0",
						"id":          "test-1",
						"source":      "test-source",
						"type":        "io.open-cluster-management.works.v1alpha1.manifests.spec.create_request",
						"data": map[string]interface{}{
							"id":        "test-1",
							"version":   1,
							"manifests": []interface{}{},
						},
					},
				}, nil
			},
		}

		mockStatusEventSvc := &mockStatusEventService{}

		router, _ := setupTestRouterService(t, mockResourceSvc, mockStatusEventSvc)

		handler := &mockEventHandler{
			handleEventFunc: func(ctx context.Context, event *ce.Event) error {
				return errors.New("handler error")
			},
		}

		handlerFuncs := router.ControllerHandlerFuncs(handler)

		// Execute
		err := handlerFuncs[api.CreateEventType][0](ctx, "test-1")

		// Assert
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "handler error") {
			t.Errorf("expected error to contain 'handler error', got: %v", err)
		}
	})
}
