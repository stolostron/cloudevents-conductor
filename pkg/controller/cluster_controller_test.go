package controller

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"github.com/openshift-online/maestro/pkg/api"
	maestromocks "github.com/openshift-online/maestro/pkg/dao/mocks"
	"github.com/openshift-online/maestro/pkg/services"
	"github.com/stolostron/cloudevents-conductor/pkg/controller/mq"
	"github.com/stolostron/cloudevents-conductor/pkg/utils/mock"
)

func TestClusterSync(t *testing.T) {
	now := metav1.Now()
	clusterName := "cluster1"
	consumerService := services.NewConsumerService(maestromocks.NewConsumerDao())
	if _, svcErr := consumerService.Create(context.Background(), &api.Consumer{
		Name: clusterName,
	}); svcErr != nil {
		t.Fatalf("failed to create consumer %s: %v", clusterName, svcErr)
	}

	cases := []struct {
		name                      string
		clusters                  []runtime.Object
		authz                     mq.MessageQueueAuthzCreator
		expectedAuthorizedCluster string
	}{
		{
			name:     "cluster not found",
			clusters: []runtime.Object{},
		},
		{
			name: "cluster is deleting",
			clusters: []runtime.Object{&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:              clusterName,
					DeletionTimestamp: &now,
				},
			}},
		},
		{
			name: "cluster is not joined",
			clusters: []runtime.Object{&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}},
		},
		{
			name: "a joined cluster (no authz)",
			clusters: []runtime.Object{&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.ManagedClusterConditionJoined,
							Status: metav1.ConditionTrue,
						},
					},
				},
			}},
		},
		{
			name: "a joined cluster",
			clusters: []runtime.Object{&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   clusterv1.ManagedClusterConditionJoined,
							Status: metav1.ConditionTrue,
						},
					},
				},
			}},
			authz:                     mock.NewMockMessageQueueAuthzCreator(),
			expectedAuthorizedCluster: clusterName,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterClient := fakeclusterclient.NewSimpleClientset(c.clusters...)
			clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, time.Minute*10)
			clusterStore := clusterInformerFactory.Cluster().V1().ManagedClusters().Informer().GetStore()
			for _, cluster := range c.clusters {
				if err := clusterStore.Add(cluster); err != nil {
					t.Fatal(err)
				}
			}

			ctrl := &ManagedClusterController{
				clusterLister:            clusterInformerFactory.Cluster().V1().ManagedClusters().Lister(),
				messageQueueAuthzCreator: c.authz,
				consumerService:          consumerService,
			}
			if err := ctrl.sync(context.Background(), mock.NewMockSyncContext(t, clusterName)); err != nil {
				t.Errorf("unexpected err: %v", err)
			}
		})
	}
}
