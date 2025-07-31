package util

import (
	"context"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func GetManagedCluster(clusterClient clusterclientset.Interface, spokeClusterName string) (*clusterv1.ManagedCluster, error) {
	spokeCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), spokeClusterName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return spokeCluster, nil
}

func GetManagedClusterLease(kubeClient kubernetes.Interface, spokeClusterName string) (*coordinationv1.Lease, error) {
	lease, err := kubeClient.CoordinationV1().Leases(spokeClusterName).Get(context.TODO(), "managed-cluster-lease", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

func AcceptManagedCluster(clusterClient clusterclientset.Interface, spokeClusterName string) error {
	return AcceptManagedClusterWithLeaseDuration(clusterClient, spokeClusterName, 60)
}

func AcceptManagedClusterWithLeaseDuration(clusterClient clusterclientset.Interface, spokeClusterName string, leaseDuration int32) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		spokeCluster, err := GetManagedCluster(clusterClient, spokeClusterName)
		if err != nil {
			return err
		}
		spokeCluster.Spec.HubAcceptsClient = true
		spokeCluster.Spec.LeaseDurationSeconds = leaseDuration
		_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), spokeCluster, metav1.UpdateOptions{})
		return err
	})
}
