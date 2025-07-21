package controller

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/controllers"
	"github.com/openshift-online/maestro/pkg/dao/mocks"
	dbmocks "github.com/openshift-online/maestro/pkg/db/mocks"
	"github.com/openshift-online/maestro/pkg/services"
)

func newTestControllerConfig(ctrl *testController) *controllers.ControllerConfig {
	return &controllers.ControllerConfig{
		Source: "test-event-source",
		Handlers: map[api.EventType][]controllers.ControllerHandlerFunc{
			api.CreateEventType: {ctrl.OnAdd},
			api.UpdateEventType: {ctrl.OnUpdate},
			api.DeleteEventType: {ctrl.OnDelete},
		},
	}
}

type testController struct {
	addCounter    int
	updateCounter int
	deleteCounter int
}

func (d *testController) OnAdd(ctx context.Context, id string) error {
	d.addCounter++
	return nil
}

func (d *testController) OnUpdate(ctx context.Context, id string) error {
	d.updateCounter++
	return nil
}

func (d *testController) OnDelete(ctx context.Context, id string) error {
	d.deleteCounter++
	return nil
}

func TestControllerFramework(t *testing.T) {
	RegisterTestingT(t)

	ctx := context.Background()
	eventsDao := mocks.NewEventDao()
	events := services.NewEventService(eventsDao)
	ctlMgr := NewControllerManager(dbmocks.NewMockAdvisoryLockFactory(), events)

	ctrl := &testController{}
	config := newTestControllerConfig(ctrl)
	ctlMgr.Add(config)

	_, _ = eventsDao.Create(ctx, &api.Event{
		Meta:      api.Meta{ID: "1"},
		Source:    config.Source,
		SourceID:  "any id",
		EventType: api.CreateEventType,
	})

	_, _ = eventsDao.Create(ctx, &api.Event{
		Meta:      api.Meta{ID: "2"},
		Source:    config.Source,
		SourceID:  "any id",
		EventType: api.UpdateEventType,
	})

	_, _ = eventsDao.Create(ctx, &api.Event{
		Meta:      api.Meta{ID: "3"},
		Source:    config.Source,
		SourceID:  "any id",
		EventType: api.DeleteEventType,
	})

	ctlMgr.handleEvent("1")
	ctlMgr.handleEvent("2")
	ctlMgr.handleEvent("3")

	Expect(ctrl.addCounter).To(Equal(1))
	Expect(ctrl.updateCounter).To(Equal(1))
	Expect(ctrl.deleteCounter).To(Equal(1))

	eve, _ := eventsDao.Get(ctx, "1")
	Expect(eve.ReconciledDate).ToNot(BeNil(), "event reconcile date should be set")
}
