package e2e

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/client/cloudevents/grpcsource"
	"github.com/stolostron/cloudevents-conductor/test/helper"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
)

var _ = Describe("Manifestwork from database", Ordered, Label("manifestwork-db"), func() {
	BeforeAll(func() {
		Eventually(func() error {
			consumerList, resp, err := maestroAPIClient.DefaultApi.ApiMaestroV1ConsumersGet(ctx).Execute()
			if err != nil {
				return fmt.Errorf("failed to get consumers: %v, response: %v", err, resp)
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}
			if consumerList == nil || len(consumerList.Items) == 0 {
				return fmt.Errorf("no consumers found")
			}
			for _, consumer := range consumerList.Items {
				if *consumer.Name == managedClusterName {
					return nil
				}
			}
			return fmt.Errorf("consumer for managed cluster %s not found", managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

	Context("Manifestwork delivered from database", func() {
		var workName string
		var resourceID string

		BeforeEach(func() {
			workName = "work-" + rand.String(5)
			work, err := helper.NewManifestWork(managedClusterName, workName, 1)
			Expect(err).ShouldNot(HaveOccurred())
			work, err = sourceWorkClient.ManifestWorks(managedClusterName).Create(ctx, work, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			// wait for few seconds to ensure the creation is finished
			<-time.After(5 * time.Second)
		})

		AfterEach(func() {
			err := sourceWorkClient.ManifestWorks(managedClusterName).Delete(ctx, workName, metav1.DeleteOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			Eventually(func() error {
				_, err := sourceWorkClient.ManifestWorks(managedClusterName).Get(ctx, workName, metav1.GetOptions{})
				if errors.IsNotFound(err) {
					return nil
				}

				if err != nil {
					return err
				}

				return fmt.Errorf("the work %s still exists", workName)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		It("should manifestwork delivered from database and apply to managed cluster via gRPC", func() {
			By("get the resource via maestro api", func() {
				// search := fmt.Sprintf("consumer_name = '%s'", managedClusterName)
				gotResourceList, resp, err := maestroAPIClient.DefaultApi.ApiMaestroV1ResourceBundlesGet(ctx).Execute()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(len(gotResourceList.Items)).To(Equal(1))
				resourceID = *gotResourceList.Items[0].Id
			})

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

			By("get the resource status from maestro", func() {
				Eventually(func() error {
					res, resp, err := maestroAPIClient.DefaultApi.ApiMaestroV1ResourceBundlesIdGet(ctx, resourceID).Execute()
					if err != nil {
						return fmt.Errorf("failed to get resource bundles: %v, response: %v", err, resp)
					}
					if resp.StatusCode != http.StatusOK {
						return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					}
					if *res.ConsumerName != managedClusterName {
						return fmt.Errorf("resource bundle for managed cluster %s not found, got %s",
							managedClusterName, *res.ConsumerName)
					}
					if res.Status == nil || len(res.Status) == 0 {
						return fmt.Errorf("resource bundle for managed cluster %s has no status", managedClusterName)
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			By("patch the resource with source work client", func() {
				work, err := sourceWorkClient.ManifestWorks(managedClusterName).Get(ctx, workName, metav1.GetOptions{})
				Expect(err).ShouldNot(HaveOccurred())

				newWork := work.DeepCopy()
				manifests, err := helper.NewManifests("default", "nginx", 2)
				Expect(err).ShouldNot(HaveOccurred())
				newWork.Spec.Workload.Manifests = manifests

				patchData, err := grpcsource.ToWorkPatch(work, newWork)
				Expect(err).ShouldNot(HaveOccurred())

				_, err = sourceWorkClient.ManifestWorks(managedClusterName).Patch(ctx, workName, types.MergePatchType, patchData, metav1.PatchOptions{})
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

				gotResource, resp, err := maestroAPIClient.DefaultApi.ApiMaestroV1ResourceBundlesIdGet(ctx, resourceID).Execute()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
				Expect(*gotResource.Version).To(Equal(int32(2)))
			})
		})
	})
})
