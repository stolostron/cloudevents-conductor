package helper

import (
	"encoding/json"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/openshift-online/maestro/pkg/api"
	"gorm.io/datatypes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	workv1 "open-cluster-management.io/api/work/v1"
	workpayload "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	cetypes "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var manifestJSON = `
{
	"apiVersion": "apps/v1",
	"kind": "Deployment",
	"metadata": {
	  "name": "%s",
	  "namespace": "%s"
	},
	"spec": {
	  "replicas": %d,
	  "selector": {
		"matchLabels": {
		  "app": "nginx"
		}
	  },
	  "template": {
		"metadata": {
		  "labels": {
			"app": "nginx"
		  }
		},
		"spec": {
		  "containers": [
			{
			  "image": "nginxinc/nginx-unprivileged",
			  "name": "nginx"
			}
		  ]
		}
	  }
	}
}
`

func newManifestJSON(name, namespace string, replicas int) string {
	return fmt.Sprintf(manifestJSON, name, namespace, replicas)
}

func encodeManifestBundle(source, manifestJSON, name, namespace string) (datatypes.JSONMap, error) {
	if len(manifestJSON) == 0 {
		return nil, nil
	}

	manifest := map[string]interface{}{}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return nil, fmt.Errorf("error unmarshalling manifest: %v", err)
	}

	// default deletion option
	delOption := &workv1.DeleteOption{
		PropagationPolicy: workv1.DeletePropagationPolicyTypeForeground,
	}

	// default update strategy
	upStrategy := &workv1.UpdateStrategy{
		Type: workv1.UpdateStrategyTypeServerSideApply,
	}

	// create a cloud event with the manifest bundle as the data
	evt := cetypes.NewEventBuilder(source, cetypes.CloudEventsType{}).NewEvent()
	eventPayload := &workpayload.ManifestBundle{
		Manifests: []workv1.Manifest{
			{
				RawExtension: runtime.RawExtension{
					Object: &unstructured.Unstructured{Object: manifest},
				},
			},
		},
		DeleteOption: delOption,
		ManifestConfigs: []workv1.ManifestConfigOption{
			{
				FeedbackRules: []workv1.FeedbackRule{
					{
						Type: workv1.JSONPathsType,
						JsonPaths: []workv1.JsonPath{
							{
								Name: "status",
								Path: ".status",
							},
						},
					},
				},
				UpdateStrategy: upStrategy,
				ResourceIdentifier: workv1.ResourceIdentifier{
					Group:     "apps",
					Resource:  "deployments",
					Name:      name,
					Namespace: namespace,
				},
			},
		},
	}

	// set the event data
	if err := evt.SetData(cloudevents.ApplicationJSON, eventPayload); err != nil {
		return nil, fmt.Errorf("failed to set cloud event data: %v", err)
	}

	// convert cloudevent to JSONMap
	manifestBundle, err := api.CloudEventToJSONMap(&evt)
	if err != nil {
		return nil, fmt.Errorf("failed to convert cloudevent to resource manifest bundle: %v", err)
	}

	return manifestBundle, nil
}

func NewResource(consumer, source string, replicas int, resourceVersion int32) (*api.Resource, error) {
	name := "nginx"
	namespace := "default" // default namespace
	manifestJSON := newManifestJSON(name, namespace, replicas)
	payload, err := encodeManifestBundle(source, manifestJSON, name, namespace)
	if err != nil {
		return nil, err
	}

	resource := &api.Resource{
		ConsumerName: consumer,
		Payload:      payload,
		Version:      resourceVersion,
	}

	return resource, nil
}

func NewManifestWork(namespace, name string, replicas int) (*workv1.ManifestWork, error) {
	manifests, err := NewManifests("default", "nginx", replicas)
	if err != nil {
		return nil, err
	}

	// default deletion option
	delOption := &workv1.DeleteOption{
		PropagationPolicy: workv1.DeletePropagationPolicyTypeForeground,
	}

	// default update strategy
	upStrategy := &workv1.UpdateStrategy{
		Type: workv1.UpdateStrategyTypeServerSideApply,
	}

	work := &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
			DeleteOption: delOption,
			ManifestConfigs: []workv1.ManifestConfigOption{
				{
					FeedbackRules: []workv1.FeedbackRule{
						{
							Type: workv1.JSONPathsType,
							JsonPaths: []workv1.JsonPath{
								{
									Name: "status",
									Path: ".status",
								},
							},
						},
					},
					UpdateStrategy: upStrategy,
					ResourceIdentifier: workv1.ResourceIdentifier{
						Group:     "apps",
						Resource:  "deployments",
						Name:      "nginx",
						Namespace: "default",
					},
				},
			},
		},
	}

	if name != "" {
		work.Name = name
	} else {
		work.GenerateName = "work-"
	}

	return work, nil
}

func NewManifests(namespace, name string, replicas int) ([]workv1.Manifest, error) {
	manifestJSON := newManifestJSON(name, namespace, replicas)

	manifest := map[string]interface{}{}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return nil, fmt.Errorf("error unmarshalling manifest: %v", err)
	}

	return []workv1.Manifest{
		{
			RawExtension: runtime.RawExtension{
				Object: &unstructured.Unstructured{Object: manifest},
			},
		},
	}, nil
}
