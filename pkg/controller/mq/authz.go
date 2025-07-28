package mq

import (
	"context"
)

const (
	BrokerGRPC = "grpc"
)

// MessageQueueAuthzCreator defines an interface for creating and deleting message queue authorization rules.
type MessageQueueAuthzCreator interface {
	CreateAuthorizations(ctx context.Context, clusterName string) error
	DeleteAuthorizations(ctx context.Context, clusterName string) error
}

// NewMessageQueueAuthzCreator creates a new instance of MessageQueueAuthzCreator.
func NewMessageQueueAuthzCreator() MessageQueueAuthzCreator {
	return &DumbAuthzCreator{}
}

// DumbAuthzCreator is a no-op implementation of MessageQueueAuthzCreator.
// It does not perform any actual authorization creation or deletion.
// This is useful for testing or when no authorization is needed.
type DumbAuthzCreator struct{}

func (d *DumbAuthzCreator) CreateAuthorizations(ctx context.Context, clusterName string) error {
	return nil
}

func (d *DumbAuthzCreator) DeleteAuthorizations(ctx context.Context, clusterName string) error {
	return nil
}
