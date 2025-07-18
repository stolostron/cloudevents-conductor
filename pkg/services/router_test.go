package services

import (
	"testing"

	"open-cluster-management.io/ocm/pkg/server/services"
)

func TestIsKubeResource(t *testing.T) {
	tests := []struct {
		name       string
		resourceID string
		want       bool
	}{
		{
			name:       "empty string",
			resourceID: "",
			want:       false,
		},
		{
			name:       "exact kube source",
			resourceID: services.CloudEventsSourceKube,
			want:       true,
		},
		{
			name:       "kube source with namespace/name",
			resourceID: services.CloudEventsSourceKube + "::ns/name",
			want:       true,
		},
		{
			name:       "non-kube source",
			resourceID: "maestro::ns/name",
			want:       false,
		},
		{
			name:       "kube prefix but not exact",
			resourceID: "kubernetes::ns/name",
			want:       false,
		},
		{
			name:       "kube source as substring",
			resourceID: "prefix" + services.CloudEventsSourceKube + "::ns/name",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKubeResource(tt.resourceID)
			if got != tt.want {
				t.Errorf("isKubeResource(%q) = %v, want %v", tt.resourceID, got, tt.want)
			}
		})
	}
}
