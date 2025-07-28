package controller

import (
	"context"
	"errors"
	"syscall"
	"time"

	"github.com/openshift-online/maestro/pkg/services"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/stolostron/cloudevents-conductor/pkg/controller/mq"
	"github.com/stolostron/cloudevents-conductor/pkg/utils/maestro"
	kubeapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisters "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// ManagedClusterController is a controller that used to create new consumers in the maestro
// when a new managed cluster is joined, and delete the consumer when the managed cluster is removed.
// It also manages message queue ACLs for the managed cluster.
type ManagedClusterController struct {
	clusterLister            clusterlisters.ManagedClusterLister
	rateLimiter              workqueue.TypedRateLimiter[string]
	messageQueueAuthzCreator mq.MessageQueueAuthzCreator
	consumerService          services.ConsumerService
}

func NewManagedClusterController(clusterInformer clusterinformers.ManagedClusterInformer,
	recorder events.Recorder,
	messageQueueAuthzCreator mq.MessageQueueAuthzCreator,
	consumerService services.ConsumerService) factory.Controller {
	controller := &ManagedClusterController{
		clusterLister:            clusterInformer.Lister(),
		rateLimiter:              workqueue.NewTypedItemExponentialFailureRateLimiter[string](5*time.Second, 300*time.Second),
		messageQueueAuthzCreator: messageQueueAuthzCreator,
		consumerService:          consumerService,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterInformer.Informer()).
		WithSync(controller.sync).
		ToController("ManagedClusterController", recorder)
}

func (c *ManagedClusterController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	logger := klog.FromContext(ctx)
	clusterName := controllerContext.QueueKey()
	logger.V(4).Info("Reconciling ManagedCluster", "managedClusterName", clusterName)

	managedCluster, err := c.clusterLister.Get(clusterName)
	if kubeapierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if !managedCluster.DeletionTimestamp.IsZero() {
		// TODO delete this cluster in the maestro
		return nil
	}

	if !meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
		// the cluster is not joined yet, do nothing
		return nil
	}

	if err := c.ensureConsumer(ctx, clusterName); err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			logger.V(4).Info("Consumer service is not available, retrying later", "clusterName", clusterName, "error", err)
			controllerContext.Queue().AddAfter(clusterName, c.rateLimiter.When(clusterName))
			return nil
		}

		return err
	}

	return c.ensureACLs(ctx, clusterName)
}

// ensureConsumer ensures that a consumer exists for the managed cluster.
func (c *ManagedClusterController) ensureConsumer(ctx context.Context, managedClusterName string) error {
	existed, err := maestro.FindConsumerByName(ctx, c.consumerService, managedClusterName)
	if err != nil {
		return err
	}

	if existed {
		return nil
	}

	// create a consumer in the maestro
	return maestro.CreateConsumer(ctx, c.consumerService, managedClusterName)
}

// ensureACLs ensures that the message queue ACLs are created for the managed cluster.
func (c *ManagedClusterController) ensureACLs(ctx context.Context, managedClusterName string) error {
	if c.messageQueueAuthzCreator != nil {
		return c.messageQueueAuthzCreator.CreateAuthorizations(ctx, managedClusterName)
	}

	return nil
}
