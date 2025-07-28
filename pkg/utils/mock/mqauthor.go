package mock

import "context"

// MockMessageQueueAuthzCreator is a mock implementation of MessageQueueAuthzCreator
// that can be used in tests to verify the behavior of components that depend on it.
// It records the cluster name for which authorizations are created or deleted.
type MockMessageQueueAuthzCreator struct {
	clusterName string
}

func NewMockMessageQueueAuthzCreator() *MockMessageQueueAuthzCreator {
	return &MockMessageQueueAuthzCreator{}
}

func (a *MockMessageQueueAuthzCreator) CreateAuthorizations(ctx context.Context, clusterName string) error {
	a.clusterName = clusterName
	return nil
}

func (a *MockMessageQueueAuthzCreator) DeleteAuthorizations(ctx context.Context, clusterName string) error {
	return nil
}

func (a *MockMessageQueueAuthzCreator) ClusterName() string {
	return a.clusterName
}
