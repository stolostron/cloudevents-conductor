package maestro

import (
	"context"
	"fmt"

	"github.com/openshift-online/maestro/pkg/api"
	"github.com/openshift-online/maestro/pkg/services"
)

// FindConsumerByName checks if a consumer with the given name exists in the maestro service.
func FindConsumerByName(ctx context.Context, consumerService services.ConsumerService, consumerName string) (bool, error) {
	consumers, svcErr := consumerService.FindByNames(ctx, []string{consumerName})
	if svcErr != nil {
		return false, fmt.Errorf("failed to get consumers by name %s: %w", consumerName, svcErr)
	}
	for _, consumer := range consumers {
		if consumer.Name == consumerName {
			return true, nil
		}
	}

	return false, nil
}

// CreateConsumer creates a new consumer in the maestro service with the given name.
func CreateConsumer(ctx context.Context, consumerService services.ConsumerService, consumerName string) error {
	if _, svcErr := consumerService.Create(ctx, &api.Consumer{
		Name: consumerName,
	}); svcErr != nil {
		return fmt.Errorf("failed to create consumer %s: %w", consumerName, svcErr)
	}

	return nil
}
