package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	dbconfig "github.com/openshift-online/maestro/pkg/config"
	"github.com/openshift-online/maestro/pkg/constants"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/clock"

	grpcserver "github.com/stolostron/cloudevents-conductor/pkg/server/grpc"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/options"

	"github.com/stolostron/cloudevents-conductor/test/integration/util"
	"open-cluster-management.io/ocm/pkg/common/helpers"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/register/grpc"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
)

const grpcTest = "grpctest"

var _ = ginkgo.Describe("Registration using GRPC", ginkgo.Ordered, ginkgo.Label("grpc-test"), func() {
	var err error

	var postfix string

	var stopGRPCServer context.CancelFunc
	var stopGRPCAgent context.CancelFunc
	var stopGRPCWorkAgent context.CancelFunc

	// var hubHash string

	// var gRPCURL string
	var gRPCServerOptions *grpcoptions.GRPCServerOptions
	var gRPCCAKeyFile string

	var bootstrapGRPCConfigFile string

	var hubOptionWithGRPC *hub.HubManagerOptions
	var hubGRPCConfigSecret string
	var hubGRPCConfigDir string

	var managedClusterName string
	var resourceID string
	// var work *workv1.ManifestWork
	// var manifests []workv1.Manifest

	ginkgo.BeforeAll(func() {
		tempDir, err := os.MkdirTemp("", fmt.Sprintf("registration-%s", grpcTest))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(tempDir).ToNot(gomega.BeEmpty())

		bootstrapGRPCConfigFile = path.Join(tempDir, "grpcconfig")
		_, gRPCServerOptions, gRPCCAKeyFile, err = util.CreateGRPCConfigs(bootstrapGRPCConfigFile)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// hubHash = helper.HubHash(gRPCURL)

		var grpcServerCtx context.Context
		grpcServerCtx, stopGRPCServer = context.WithCancel(context.Background())

		serverConfig := &grpcserver.GRPCServerConfig{
			GRPCConfig: gRPCServerOptions,
			DBConfig:   dbconfig.NewDatabaseConfig(),
		}
		serverConfig.DBConfig.Host = "localhost"
		serverConfig.DBConfig.Port = int(testPostgres.Port)
		serverConfig.DBConfig.Name = "maestro"
		serverConfig.DBConfig.Username = "postgres"
		serverConfig.DBConfig.Password = "postgres"
		serverConfig.DBConfig.SSLMode = "disable"

		serverConfigBytes, err := yaml.Marshal(serverConfig)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		serverConfigFile := path.Join(tempDir, "server-config.yaml")
		err = os.WriteFile(serverConfigFile, serverConfigBytes, 0644)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		grpcServerOptions := grpcserver.NewGRPCServerOptions()
		grpcServerOptions.GRPCServerConfigFile = serverConfigFile
		go func() {
			err := grpcServerOptions.Run(grpcServerCtx, &controllercmd.ControllerContext{
				KubeConfig:    hubCfg,
				EventRecorder: events.NewKubeRecorder(hubKubeClient.CoreV1().Events("default"), "grpc-server", nil, clock.RealClock{}),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		// wait for the grpc server to be ready
		gomega.Eventually(func() error {
			_, err := net.DialTimeout("tcp", "localhost:8090", 5*time.Second)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// registrationHook, err := util.NewGRPCServerRegistrationHook(hubCfg)
		// gomega.Expect(err).NotTo(gomega.HaveOccurred())
		// workHook, err := util.NewGRPCServerWorkHook(hubCfg)
		// gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// server := grpcoptions.NewServer(gRPCServerOptions).WithPreStartHooks(registrationHook).WithPreStartHooks(workHook).WithService(
		// 	clusterce.ManagedClusterEventDataType,
		// 	cluster.NewClusterService(registrationHook.ClusterClient, registrationHook.ClusterInformers.Cluster().V1().ManagedClusters()),
		// ).WithService(
		// 	csrce.CSREventDataType,
		// 	csr.NewCSRService(registrationHook.KubeClient, registrationHook.KubeInformers.Certificates().V1().CertificateSigningRequests()),
		// ).WithService(
		// 	addonce.ManagedClusterAddOnEventDataType,
		// 	addon.NewAddonService(registrationHook.AddOnClient, registrationHook.AddOnInformers.Addon().V1alpha1().ManagedClusterAddOns()),
		// ).WithService(
		// 	eventce.EventEventDataType,
		// 	event.NewEventService(registrationHook.KubeClient),
		// ).WithService(
		// 	leasece.LeaseEventDataType,
		// 	lease.NewLeaseService(registrationHook.KubeClient, registrationHook.KubeInformers.Coordination().V1().Leases()),
		// ).WithService(
		// 	payload.ManifestBundleEventDataType,
		// 	serviceswork.NewWorkService(workHook.WorkClient, workHook.WorkInformers.Work().V1().ManifestWorks()),
		// )

		// go func() {
		// 	err := server.Run(grpcServerCtx)
		// 	gomega.Expect(err).NotTo(gomega.HaveOccurred())
		// }()
	})

	ginkgo.AfterAll(func() {
		stopGRPCServer()
	})

	ginkgo.Context("ManagedCluster registration", func() {
		ginkgo.BeforeEach(func() {
			postfix = rand.String(5)
			hubOptionWithGRPC = hub.NewHubManagerOptions()
			hubOptionWithGRPC.EnabledRegistrationDrivers = []string{helpers.GRPCCAuthType}
			hubOptionWithGRPC.GRPCCAFile = gRPCServerOptions.ClientCAFile
			hubOptionWithGRPC.GRPCCAKeyFile = gRPCCAKeyFile
			startHub(hubOptionWithGRPC)

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
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(ns).ToNot(gomega.BeNil())

			grpcCommOptions := commonoptions.NewAgentOptions()
			grpcCommOptions.HubKubeconfigDir = hubGRPCConfigDir
			grpcCommOptions.SpokeClusterName = managedClusterName
			stopGRPCAgent = runAgent(fmt.Sprintf("%s-grpc-agent", grpcTest), grpcAgentOptions, grpcCommOptions, spokeCfg)
			stopGRPCWorkAgent = runWorkAgent(fmt.Sprintf("%s-grpc-work-agent", grpcTest), grpcWorkAgentOptions, grpcCommOptions, spokeCfg)
		})

		ginkgo.AfterEach(func() {
			stopGRPCAgent()
			stopGRPCWorkAgent()
			stopHub()
		})

		ginkgo.It("should register managedclusters and apply manifestwork with grpc successfully", func() {
			ginkgo.By("getting managedclusters and csrs after bootstrap", func() {
				assertManagedCluster(managedClusterName)
			})

			// simulate hub cluster admin to accept the managedcluster and approve the csr
			ginkgo.By("accept managedclusters and approve csrs", func() {
				err = util.AcceptManagedCluster(hubClusterClient, managedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// for gpc, the hub controller will sign the client certs, we just approve
				// the csr here
				csr, err := util.FindUnapprovedSpokeCSR(hubKubeClient, managedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				err = util.ApproveCSR(hubKubeClient, csr)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			})

			ginkgo.By("getting managedclusters joined condition", func() {
				assertManagedClusterJoined(managedClusterName, hubGRPCConfigSecret)
			})

			ginkgo.By("getting managedclusters available condition constantly", func() {
				// after two short grace period, make sure the managed cluster is available
				gracePeriod := 2 * 5 * util.TestLeaseDurationSeconds
				assertAvailableCondition(managedClusterName, metav1.ConditionTrue, gracePeriod)
			})

			ginkgo.By("getting consumer for the managedcluster", func() {
				gomega.Eventually(func() error {
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
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("creating manifestwork with grpc", func() {
				res, err := util.NewResource(managedClusterName, constants.DefaultSourceID, 1, 1)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(res).ToNot(gomega.BeNil())
				res, svcErr := resources.Create(context.Background(), res)
				gomega.Expect(svcErr).ToNot(gomega.HaveOccurred())
				gomega.Expect(res).ToNot(gomega.BeNil())
				resourceID = res.ID

				gomega.Eventually(func() error {
					_, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
					return err
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

				deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				for _, entry := range deploy.ManagedFields {
					gomega.Expect(entry.Manager).To(gomega.Equal("work-agent"))
				}
				gomega.Expect(*deploy.Spec.Replicas).To(gomega.Equal(int32(1)))

				gomega.Eventually(func() error {
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
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("updating manifestwork with grpc", func() {
				res, err := util.NewResource(managedClusterName, constants.DefaultSourceID, 2, 1)
				res.ID = resourceID
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(res).ToNot(gomega.BeNil())
				res, svcErr := resources.Update(context.Background(), res)
				gomega.Expect(svcErr).ToNot(gomega.HaveOccurred())
				gomega.Expect(res).ToNot(gomega.BeNil())

				gomega.Eventually(func() error {
					deploy, err := spokeKubeClient.AppsV1().Deployments("default").Get(context.Background(), "nginx", metav1.GetOptions{})
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
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			ginkgo.By("deleting manifestwork with grpc", func() {
				svcErr := resources.MarkAsDeleting(context.Background(), resourceID)
				gomega.Expect(svcErr).ToNot(gomega.HaveOccurred())
				gomega.Eventually(func() error {
					_, err := spokeKubeClient.AppsV1().Deployments("default").
						Get(context.Background(), "nginx", metav1.GetOptions{})
					if err == nil {
						return fmt.Errorf("deployment nginx still exists")
					}
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				}, eventuallyTimeout, eventuallyInterval).Should(gomega.HaveOccurred())

				gomega.Eventually(func() error {
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
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})
		})
	})
})

func assertManagedCluster(clusterName string) {
	gomega.Eventually(func() error {
		if _, err := util.GetManagedCluster(hubClusterClient, clusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	gomega.Eventually(func() error {
		if _, err := util.FindUnapprovedSpokeCSR(hubKubeClient, clusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// the spoke cluster should has finalizer that is added by hub controller
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, clusterName)
		if err != nil {
			return err
		}

		if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
			return fmt.Errorf("finalizer is not correct")
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertManagedClusterJoined(clusterName, hubConfigSecret string) {
	// the managed cluster should have accepted condition after it is accepted
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, clusterName)
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return fmt.Errorf("cluster should be accepted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// the hub kubeconfig secret should be filled after the csr is approved
	gomega.Eventually(func() error {
		_, err := util.GetFilledHubKubeConfigSecret(hubKubeClient, testNamespace, hubConfigSecret)
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// the spoke cluster should have joined condition finally
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, clusterName)
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
			return fmt.Errorf("cluster should be joined")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertAvailableCondition(managedClusterName string, status metav1.ConditionStatus, d int) {
	<-time.After(time.Duration(d) * time.Second)
	gomega.Eventually(func() error {
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
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}
