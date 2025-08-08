package maestro

import (
	"context"
	"testing"

	"github.com/openshift-online/maestro/pkg/api"
	maestromocks "github.com/openshift-online/maestro/pkg/dao/mocks"
	"github.com/openshift-online/maestro/pkg/services"
)

func TestFindConsumerByName(t *testing.T) {
	ctx := context.Background()
	defer ctx.Done()
	consumerName := "cluster1"
	consumerService := services.NewConsumerService(maestromocks.NewConsumerDao())
	if _, svcErr := consumerService.Create(ctx, &api.Consumer{
		Name: consumerName,
	}); svcErr != nil {
		t.Fatalf("failed to create consumer %s: %v", consumerName, svcErr)
	}

	cases := []struct {
		name        string
		consumer    string
		expected    bool
		expectedErr bool
	}{
		{
			name:        "find an existed consumer",
			consumer:    consumerName,
			expected:    true,
			expectedErr: false,
		},
		{
			name:        "find a nonexistent consumer",
			consumer:    "nonexistent",
			expected:    false,
			expectedErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := FindConsumerByName(
				ctx, consumerService, c.consumer)
			if err != nil && !c.expectedErr {
				t.Errorf("unexpected error: %v", err)
			}

			if result != c.expected {
				t.Errorf("unexpected result: %t", result)
			}
		})
	}
}

func TestCreateConsumer(t *testing.T) {
	ctx := context.Background()
	defer ctx.Done()
	consumerService := services.NewConsumerService(maestromocks.NewConsumerDao())

	if err := CreateConsumer(ctx, consumerService, "test"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

}
