package integration

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/constants"
	"github.com/stolostron/cloudevents-conductor/test/helper"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/register/grpc"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

const grpcTest = "grpctest"

var _ = Describe("Registration and apply work using GRPC", Ordered, Label("grpc-test"), func() {
	var err error

	var postfix string
	var managedClusterName string

	var stopGRPCRegistrationAgent context.CancelFunc
	var stopGRPCWorkAgent context.CancelFunc

	var hubGRPCConfigSecret string
	var hubGRPCConfigDir string

	var resourceID string
	var work *workv1.ManifestWork

	Context("ManagedCluster registration", func() {
		BeforeEach(func() {
			postfix = rand.String(5)

			managedClusterName = fmt.Sprintf("%s-managedcluster-grpc-%s", grpcTest, postfix)

			hubGRPCConfigSecret = fmt.Sprintf("%s-hub-grpcconfig-secret-%s", grpcTest, postfix)
			hubGRPCConfigDir = path.Join(util.TestDir, fmt.Sprintf("%s-grpc-%s", grpcTest, postfix), "hub-kubeconfig")
			grpcDriverOption := factory.NewOptions()
			grpcDriverOption.RegistrationAuth = helpers.GRPCCAuthType
			grpcDriverOption.GRPCOption = &grpc.Option{
				BootstrapConfigFile: bootstrapGRPCConfigFile,
				ConfigFile:          path.Join(hubGRPCConfigDir, "config.yaml"),
			}
			grpcAgentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubGRPCConfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     grpcDriverOption,
			}

			grpcWorkAgentOptions := workspoke.NewWorkloadAgentOptions()
			grpcWorkAgentOptions.StatusSyncInterval = 3 * time.Second
			grpcWorkAgentOptions.AppliedManifestWorkEvictionGracePeriod = 5 * time.Second
			grpcWorkAgentOptions.WorkloadSourceDriver = "grpc"
			grpcWorkAgentOptions.WorkloadSourceConfig = bootstrapGRPCConfigFile
			grpcWorkAgentOptions.CloudEventsClientID = fmt.Sprintf("%s-work-agent", managedClusterName)
			grpcWorkAgentOptions.CloudEventsClientCodecs = []string{"manifestbundle"}
			ns, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: managedClusterName,
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(ns).ToNot(BeNil())

			grpcCommOptions := commonoptions.NewAgentOptions()
			grpcCommOptions.HubKubeconfigDir = hubGRPCConfigDir
			grpcCommOptions.SpokeClusterName = managedClusterName
			stopGRPCRegistrationAgent = runAgent(fmt.Sprintf("%s-grpc-agent", grpcTest), grpcAgentOptions, grpcCommOptions, spokeCfg)
			stopGRPCWorkAgent = runWorkAgent(fmt.Sprintf("%s-grpc-work-agent", grpcTest), grpcWorkAgentOptions, grpcCommOptions, spokeCfg)
		})

		AfterEach(func() {
			stopGRPCRegistrationAgent()
			stopGRPCWorkAgent()
		})

		It("should register managedclusters and apply manifestwork with grpc successfully", func() {
			By("getting managedclusters and csrs after bootstrap", func() {
				assertManagedCluster(managedClusterName)
			})

			// simulate hub cluster admin to accept the managedcluster and approve the csr
			By("accept managedclusters and approve csrs", func() {
				err = util.AcceptManagedCluster(hubClusterClient, managedClusterName)
				Expect(err).NotTo(HaveOccurred())

				// for gpc, the hub controller will sign the client certs, we just approve
				// the csr here
				csr, err := util.FindUnapprovedSpokeCSR(hubKubeClient, managedClusterName)
				Expect(err).NotTo(HaveOccurred())

				err = util.ApproveCSR(hubKubeClient, csr)
				Expect(err).NotTo(HaveOccurred())
			})

			By("getting managedclusters joined condition", func() {
				assertManagedClusterJoined(managedClusterName, hubGRPCConfigSecret)
			})

			By("getting managedclusters available condition constantly", func() {
				// after two short grace period, make sure the managed cluster is available
				gracePeriod := 2 * 5 * util.TestLeaseDurationSeconds
				assertAvailableCondition(managedClusterName, metav1.ConditionTrue, gracePeriod)
			})

			By("getting consumer for the managedcluster", func() {
				Eventually(func() error {
					consumerList, resp, err := openAPIClient.DefaultApi.ApiMaestroV1ConsumersGet(context.Background()).Execute()
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

			By("creating manifestwork from db", func() {
				res, err := helper.NewResource(managedClusterName, constants.DefaultSourceID, 1, 1)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())
				res, svcErr := resourceService.Create(context.Background(), res)
				Expect(svcErr).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())
				resourceID = res.ID

				Eventually(func() error {
					_, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

				deploy, err := spokeKubeClient.AppsV1().Deployments("default").
					Get(context.Background(), "nginx", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				for _, entry := range deploy.ManagedFields {
					Expect(entry.Manager).To(Equal("work-agent"))
				}
				Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

				Eventually(func() error {
					resList, resp, err := openAPIClient.DefaultApi.ApiMaestroV1ResourceBundlesGet(context.Background()).Execute()
					if err != nil {
						return fmt.Errorf("failed to get resource bundles: %v, response: %v", err, resp)
					}
					if resp.StatusCode != http.StatusOK {
						return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					}
					if resList == nil || len(resList.Items) != 1 {
						return fmt.Errorf("no resource bundles found")
					}
					rb := resList.Items[0]
					if *rb.ConsumerName != managedClusterName {
						return fmt.Errorf("resource bundle for managed cluster %s not found, got %s",
							managedClusterName, *rb.ConsumerName)
					}
					if rb.Status == nil || len(rb.Status) == 0 {
						return fmt.Errorf("resource bundle for managed cluster %s has no status", managedClusterName)
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
			})

			By("updating manifestwork from db", func() {
				res, err := helper.NewResource(managedClusterName, constants.DefaultSourceID, 2, 1)
				res.ID = resourceID
				Expect(err).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())
				res, svcErr := resourceService.Update(context.Background(), res)
				Expect(svcErr).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())

				Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("failed to get deployment nginx: %v", err)
					}
					if deploy == nil {
						return fmt.Errorf("deployment nginx not found")
					}
					if *deploy.Spec.Replicas != 2 {
						return fmt.Errorf("expected 2 replicas, got %d", *deploy.Spec.Replicas)
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
			})

			By("deleting manifestwork with grpc", func() {
				svcErr := resourceService.MarkAsDeleting(context.Background(), resourceID)
				Expect(svcErr).ToNot(HaveOccurred())
				Eventually(func() error {
					_, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					if err == nil {
						return fmt.Errorf("deployment nginx still exists")
					}
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				}, eventuallyTimeout, eventuallyInterval).Should(HaveOccurred())

				Eventually(func() error {
					resList, resp, err := openAPIClient.DefaultApi.ApiMaestroV1ResourceBundlesGet(context.Background()).Execute()
					if err != nil {
						return fmt.Errorf("failed to get resource bundles: %v, response: %v", err, resp)
					}
					if resp.StatusCode != http.StatusOK {
						return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					}
					if resList == nil || len(resList.Items) != 0 {
						return fmt.Errorf("resource bundles still exist after deletion")
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
			})

			By("creating manifestwork from kube", func() {
				var err error
				work, err = helper.NewManifestWork(managedClusterName, "", 1)
				Expect(err).ToNot(HaveOccurred())
				work, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Create(context.Background(), work, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() error {
					_, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

				deploy, err := spokeKubeClient.AppsV1().Deployments("default").
					Get(context.Background(), "nginx", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				for _, entry := range deploy.ManagedFields {
					Expect(entry.Manager).To(Equal("work-agent"))
				}
				Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

				// ensure status is updated on hub work client
				Eventually(func() error {
					updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("failed to get manifestwork: %v", err)
					}
					if len(updatedWork.Status.ResourceStatus.Manifests) == 0 {
						return fmt.Errorf("manifestwork status is not updated")
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
			})

			By("updating manifestwork from kube", func() {
				newManifests, err := helper.NewManifests("default", "nginx", 2)
				Expect(err).ToNot(HaveOccurred())

				updatedWork, err := hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				newWork := updatedWork.DeepCopy()
				newWork.Spec.Workload.Manifests = newManifests

				pathBytes, err := util.NewWorkPatch(updatedWork, newWork)
				Expect(err).ToNot(HaveOccurred())

				_, err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Patch(
					context.Background(), updatedWork.Name, types.MergePatchType, pathBytes, metav1.PatchOptions{})
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("failed to get deployment nginx: %v", err)
					}
					if deploy == nil {
						return fmt.Errorf("deployment nginx not found")
					}
					if *deploy.Spec.Replicas != 2 {
						return fmt.Errorf("expected 2 replicas, got %d", *deploy.Spec.Replicas)
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
			})

			By("deleting manifestwork from kube", func() {
				err = hubWorkClient.WorkV1().ManifestWorks(managedClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() error {
					_, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					if err == nil {
						return fmt.Errorf("deployment nginx still exists")
					}
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				}, eventuallyTimeout, eventuallyInterval).Should(HaveOccurred())
			})
		})
	})
})

func assertManagedCluster(clusterName string) {
	Eventually(func() error {
		if _, err := util.GetManagedCluster(hubClusterClient, clusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

	Eventually(func() error {
		if _, err := util.FindUnapprovedSpokeCSR(hubKubeClient, clusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

	// the spoke cluster should has finalizer that is added by hub controller
	Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, clusterName)
		if err != nil {
			return err
		}

		if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
			return fmt.Errorf("finalizer is not correct")
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertManagedClusterJoined(clusterName, hubConfigSecret string) {
	// the managed cluster should have accepted condition after it is accepted
	Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, clusterName)
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return fmt.Errorf("cluster should be accepted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

	// the hub kubeconfig secret should be filled after the csr is approved
	Eventually(func() error {
		_, err := util.GetFilledHubKubeConfigSecret(hubKubeClient, testNamespace, hubConfigSecret)
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

	// the spoke cluster should have joined condition finally
	Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, clusterName)
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
			return fmt.Errorf("cluster should be joined")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}

func assertAvailableCondition(managedClusterName string, status metav1.ConditionStatus, d int) {
	<-time.After(time.Duration(d) * time.Second)
	Eventually(func() error {
		managedCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
		if err != nil {
			return err
		}
		availableCond := meta.FindStatusCondition(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable)
		if availableCond == nil {
			return fmt.Errorf("available condition is not found")
		}
		if availableCond.Status != status {
			return fmt.Errorf("expected avaibale condition is %s, but %v", status, availableCond)
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
}
