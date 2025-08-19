package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/client/cloudevents/grpcsource"
	"github.com/stolostron/cloudevents-conductor/test/helper"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	v1 "open-cluster-management.io/api/cluster/v1"
)

var _ = Describe("Manifestwork from kubernetes", Ordered, Label("manifestwork-kube"), func() {
	BeforeAll(func() {
		Eventually(func() error {
			mc, err := hubClusterClient.ClusterV1().ManagedClusters().Get(ctx, managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(mc.Status.Conditions, v1.ManagedClusterConditionHubAccepted) {
				return fmt.Errorf("managed cluster %s should have hub accepted condition", managedClusterName)
			}
			if !meta.IsStatusConditionTrue(mc.Status.Conditions, v1.ManagedClusterConditionJoined) {
				return fmt.Errorf("managed cluster %s should have joined condition", managedClusterName)
			}
			if !meta.IsStatusConditionTrue(mc.Status.Conditions, v1.ManagedClusterConditionAvailable) {
				return fmt.Errorf("managed cluster %s should have available condition", managedClusterName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	Context("Manifestwork delivered from kubernetes", func() {
		var workName string

		BeforeEach(func() {
			workName = "work-" + rand.String(5)
			work, err := helper.NewManifestWork(managedClusterName, workName, 1)
			Expect(err).ShouldNot(HaveOccurred())
			work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Delete(ctx, workName, metav1.DeleteOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			Eventually(func() error {
				_, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(ctx, workName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}

				if err != nil {
					return err
				}

				return fmt.Errorf("the work %s still exists", workName)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should manifestwork delivered from kubernetes and apply to managed cluster via gRPC", func() {
			By("get the kube resource from managed cluster", func() {
				Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(ctx, "nginx", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if *deploy.Spec.Replicas != 1 {
						return fmt.Errorf("unexpected replicas, expected 1, got %d", *deploy.Spec.Replicas)
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			})

			By("get the resource status from hub work client", func() {
				Eventually(func() error {
					work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(ctx, workName, metav1.GetOptions{})
					if err != nil {
						return err
					}

					if len(work.Status.ResourceStatus.Manifests) == 0 {
						return fmt.Errorf("the work %s has no resource status", workName)
					}

					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			By("patch the resource with hub work client", func() {
				work, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(ctx, workName, metav1.GetOptions{})
				Expect(err).ShouldNot(HaveOccurred())

				newWork := work.DeepCopy()
				manifests, err := helper.NewManifests("default", "nginx", 2)
				Expect(err).ShouldNot(HaveOccurred())
				newWork.Spec.Workload.Manifests = manifests

				patchData, err := grpcsource.ToWorkPatch(work, newWork)
				Expect(err).ShouldNot(HaveOccurred())

				_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Patch(ctx, workName, types.MergePatchType, patchData, metav1.PatchOptions{})
				Expect(err).ShouldNot(HaveOccurred())

				Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(ctx, "nginx", metav1.GetOptions{})
					if err != nil {
						return err
					}
					if *deploy.Spec.Replicas != 2 {
						return fmt.Errorf("unexpected replicas, expected 2, got %d", *deploy.Spec.Replicas)
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})
		})
	})
})
