package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/controllers"
	"github.com/openshift-online/maestro/pkg/db"
	"github.com/openshift-online/maestro/pkg/services"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

/*
This controller pattern mimics upstream Kube-style controllers with Add/Update/Delete events with periodic
sync-the-world for any messages missed.

The implementation is specific to the Event table in maestro service and leverages features of PostgreSQL:

	1. pg_notify(channel, msg) is used for real time notification to listeners
	2. advisory locks are used for concurrency when doing background work

DAOs decorated similarly to the ResourceDAO will persist Events to the database and listeners are notified of the changed.
A worker attempting to process the Event will first obtain a fail-fast advisory lock. Of many competing workers, only
one would first successfully obtain the lock. All other workers will *not* wait to obtain the lock.

Any successful processing of an Event will remove it from the Events table permanently.

A periodic process reads from the Events table and calls pg_notify, ensuring any failed Events are re-processed. Competing
consumers for the lock will fail fast on redundant messages.

*/

// defaultEventsSyncPeriod is a default events sync period (10 hours)
// given a long period because we have a queue in the controller, it will help us to handle most expected errors, this
// events sync will help us to handle unexpected errors (e.g. sever restart), it ensures we will not miss any events
var defaultEventsSyncPeriod = 10 * time.Hour

// ControllerManager is responsible for managing event controllers.
type ControllerManager struct {
	controllers map[string]map[api.EventType][]controllers.ControllerHandlerFunc
	lockFactory db.LockFactory
	events      services.EventService
	eventsQueue workqueue.RateLimitingInterface
}

func NewControllerManager(lockFactory db.LockFactory, events services.EventService) *ControllerManager {
	return &ControllerManager{
		controllers: map[string]map[api.EventType][]controllers.ControllerHandlerFunc{},
		lockFactory: lockFactory,
		events:      events,
		eventsQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "event-controller"),
	}
}

func (cm *ControllerManager) Queue() workqueue.RateLimitingInterface {
	return cm.eventsQueue
}

func (cm *ControllerManager) Add(config *controllers.ControllerConfig) {
	for ev, fn := range config.Handlers {
		cm.add(config.Source, ev, fn)
	}
}

func (cm *ControllerManager) AddEvent(id string) {
	cm.eventsQueue.Add(id)
}

// Run starts the controller manager in a goroutine and starts processing events.
func (cm *ControllerManager) Run(ctx context.Context) {
	go func(ctx context.Context) {
		klog.Infof("Starting event controller")
		defer cm.eventsQueue.ShutDown()

		// start a goroutine to sync all events periodically
		// use a jitter to avoid multiple instances syncing the events at the same time
		go wait.JitterUntil(cm.syncEvents, defaultEventsSyncPeriod, 0.25, true, ctx.Done())

		// start a goroutine to handle the event from the event queue
		// the .Until will re-kick the runWorker one second after the runWorker completes
		go wait.Until(cm.runWorker, time.Second, ctx.Done())

		// wait until context is done
		<-ctx.Done()
		klog.Infof("Shutting down event controller")
	}(ctx)
}

func (cm *ControllerManager) add(source string, ev api.EventType, fns []controllers.ControllerHandlerFunc) {
	if _, exists := cm.controllers[source]; !exists {
		cm.controllers[source] = map[api.EventType][]controllers.ControllerHandlerFunc{}
	}

	if _, exists := cm.controllers[source][ev]; !exists {
		cm.controllers[source][ev] = []controllers.ControllerHandlerFunc{}
	}

	cm.controllers[source][ev] = append(cm.controllers[source][ev], fns...)
}

func (cm *ControllerManager) handleEvent(id string) (bool, error) {
	ctx := context.Background()
	// lock the Event with a fail-fast advisory lock context.
	// this allows concurrent processing of many events by one or many controller managers.
	// allow the lock to be released by the handler goroutine and allow this function to continue.
	// subsequent events will be locked by their own distinct IDs.
	lockOwnerID, acquired, err := cm.lockFactory.NewNonBlockingLock(ctx, id, db.Events)
	// Ensure that the transaction related to this lock always end.
	defer cm.lockFactory.Unlock(ctx, lockOwnerID)
	if err != nil {
		return false, fmt.Errorf("error obtaining the event lock: %v", err)
	}

	if !acquired {
		klog.Infof("Event %s is processed by another worker, continue to process the next", id)
		return true, nil
	}

	reqContext := context.WithValue(ctx, controllers.EventID, id)

	event, svcErr := cm.events.Get(reqContext, id)
	if svcErr != nil {
		if svcErr.Is404() {
			// the event is already deleted, we can ignore it
			return true, nil
		}
		return false, fmt.Errorf("error getting event with id(%s): %s", id, svcErr)
	}

	if event.ReconciledDate != nil {
		// the event is already reconciled, we can ignore it
		klog.Infof("Event with id (%s) is already reconciled", id)
		return true, nil
	}

	source, found := cm.controllers[event.Source]
	if !found {
		klog.Infof("No controllers found for '%s'\n", event.Source)
		return true, nil
	}

	handlerFns, found := source[event.EventType]
	if !found {
		klog.Infof("No handler functions found for '%s-%s'\n", event.Source, event.EventType)
		return true, nil
	}
	for _, fn := range handlerFns {
		err := fn(reqContext, event.SourceID)
		if err != nil {
			return false, fmt.Errorf("error handing event %s, %s, %s: %s", event.Source, event.EventType, id, err)
		}
	}

	// all handlers successfully executed
	now := time.Now()
	event.ReconciledDate = &now
	_, svcErr = cm.events.Replace(reqContext, event)
	if svcErr != nil {
		return false, fmt.Errorf("error updating event with id (%s): %s", id, svcErr)
	}

	// the event is reconciled, we can ignore it
	return true, nil
}

func (cm *ControllerManager) runWorker() {
	// hot loop until we're told to stop. processNextEvent will automatically wait until there's work available, so
	// we don't worry about secondary waits
	for cm.processNextEvent() {
	}
}

// processNextEvent deals with one key off the queue.
func (cm *ControllerManager) processNextEvent() bool {
	// pull the next event item from queue.
	// events queue blocks until it can return an item to be processed
	key, quit := cm.eventsQueue.Get()
	if quit {
		// the current queue is shutdown and becomes empty, quit this process
		return false
	}
	defer cm.eventsQueue.Done(key)

	if reconciled, err := cm.handleEvent(key.(string)); !reconciled {
		if err != nil {
			klog.Errorf("Failed to handle the event %v, %v ", key, err)
		}

		// the event is not reconciled, we requeue it to work on later
		// this method will add a backoff to avoid hotlooping on particular items
		cm.eventsQueue.AddRateLimited(key)
		return true
	}

	// we handle the event successfully, tell the queue to stop tracking history for this event
	cm.eventsQueue.Forget(key)
	return true
}

func (cm *ControllerManager) syncEvents() {
	klog.Infof("purge all reconciled events")
	// delete the reconciled events from the database firstly
	if err := cm.events.DeleteAllReconciledEvents(context.Background()); err != nil {
		// this process is called periodically, so if the error happened, we will wait for the next cycle to handle
		// this again
		klog.Errorf("Failed to delete reconciled events from db: %v", err)
		return
	}

	klog.Infof("sync all unreconciled events")
	unreconciledEvents, err := cm.events.FindAllUnreconciledEvents(context.Background())
	if err != nil {
		klog.Errorf("Failed to list unreconciled events from db: %v", err)
		return
	}

	// add the unreconciled events back to the controller queue
	for _, event := range unreconciledEvents {
		cm.eventsQueue.Add(event.ID)
	}
}
