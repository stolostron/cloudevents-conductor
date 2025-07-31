package mock

import (
	"testing"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	"k8s.io/client-go/util/workqueue"
)

// MockSyncContext is a mock implementation of factory.SyncContext
// that can be used in tests to verify the behavior of components that depend on it.
// It provides a mock queue and recorder, allowing you to simulate the behavior of a sync context
// without needing a real Kubernetes environment.
type MockSyncContext struct {
	key      string
	recorder events.Recorder
	queue    workqueue.RateLimitingInterface // nolint:staticcheck
}

var _ factory.SyncContext = &MockSyncContext{}

// nolint:staticcheck
func (m MockSyncContext) Queue() workqueue.RateLimitingInterface { return m.queue }
func (m MockSyncContext) QueueKey() string                       { return m.key }
func (m MockSyncContext) Recorder() events.Recorder              { return m.recorder }

func NewMockSyncContext(t *testing.T, key string) *MockSyncContext {
	return &MockSyncContext{
		key:      key,
		recorder: eventstesting.NewTestingEventRecorder(t),
		// nolint:staticcheck
		queue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}
