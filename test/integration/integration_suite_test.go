package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/api/openapi"
	dbconfig "github.com/openshift-online/maestro/pkg/config"
	"github.com/openshift-online/maestro/pkg/services"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/stolostron/cloudevents-conductor/test/integration/testpostgres"
	"gopkg.in/yaml.v2"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/stolostron/cloudevents-conductor/pkg/server/grpc"
	"github.com/stolostron/cloudevents-conductor/test/integration/maestro"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
	grpcserver "open-cluster-management.io/sdk-go/pkg/server/grpc"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var hubCfg, spokeCfg *rest.Config

var testEnv *envtest.Environment
var securePort string
var serverCertFile string
var authn *util.TestAuthn

var hubKubeClient kubernetes.Interface
var hubClusterClient clusterclientset.Interface
var hubWorkClient workclientset.Interface
var spokeKubeClient kubernetes.Interface

var testNamespace string

var bootstrapKubeConfigFile string
var bootstrapGRPCConfigFile string

var stopHub context.CancelFunc

var stopGRPCServer context.CancelFunc

var gRPCServerOptions *grpcserver.GRPCServerOptions
var gRPCCAKeyFile string

var testPostgres *testpostgres.TestPostgres

var maestroInstance *maestro.Maestro
var maestroCtx context.Context
var maestroCancel context.CancelFunc
var openAPIClient *openapi.APIClient
var resourceService services.ResourceService

var CRDPaths = []string{
	// hub
	"./test/integration/CRDs/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	"./test/integration/CRDs/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
	"./test/integration/CRDs/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"./test/integration/CRDs/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
	"./test/integration/CRDs/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
	// spoke
	"./test/integration/CRDs/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	"./test/integration/CRDs/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
	// external API deps
	"./test/integration/CRDs/cluster.x-k8s.io_clusters.yaml",
}

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	ginkgo.By("bootstrapping test environment")

	var err error

	// crank up the sync speed
	transport.CertCallbackRefreshDuration = 5 * time.Second
	register.ControllerResyncInterval = 5 * time.Second
	registration.CreatingControllerSyncInterval = 1 * time.Second

	// crank up the addon lease sync and update speed
	spoke.AddOnLeaseControllerSyncInterval = 5 * time.Second
	addon.AddOnLeaseControllerLeaseDurationSeconds = 1

	// install cluster CRD and start a local kube-apiserver
	authn = util.DefaultTestAuthn
	apiserver := &envtest.APIServer{}
	apiserver.SecureServing.Authn = authn

	testEnv = &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: apiserver,
		},
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// bootstrap rbac for cloudevents authorization
	gomega.Expect(bootstrapRBAC(context.Background(), cfg)).NotTo(gomega.HaveOccurred())

	features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates)
	features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)
	features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates)

	err = clusterv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = workv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare configs
	securePort = testEnv.ControlPlane.APIServer.SecureServing.Port
	gomega.Expect(len(securePort)).ToNot(gomega.BeZero())

	serverCertFile = fmt.Sprintf("%s/apiserver.crt", testEnv.ControlPlane.APIServer.CertDir)

	hubCfg = cfg
	spokeCfg = cfg
	gomega.Expect(spokeCfg).ToNot(gomega.BeNil())

	bootstrapKubeConfigFile = path.Join(util.TestDir, "bootstrap", "kubeconfig")
	err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, serverCertFile, securePort, 24*time.Hour)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	bootstrapGRPCConfigFile = path.Join(util.TestDir, "bootstrap", "grpcconfig")
	_, gRPCServerOptions, gRPCCAKeyFile, err = util.CreateGRPCConfigs(bootstrapGRPCConfigFile)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare clients
	hubKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(hubKubeClient).ToNot(gomega.BeNil())

	hubClusterClient, err = clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(hubClusterClient).ToNot(gomega.BeNil())

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(hubWorkClient).ToNot(gomega.BeNil())

	spokeKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(spokeKubeClient).ToNot(gomega.BeNil())

	// prepare test namespace
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		testNamespace = "open-cluster-management-agent"
	} else {
		testNamespace = string(nsBytes)
	}
	err = util.PrepareSpokeAgentNamespace(spokeKubeClient, testNamespace)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable DefaultClusterSet feature gate
	err = features.HubMutableFeatureGate.Set("DefaultClusterSet=true")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable ManagedClusterAutoApproval feature gate
	err = features.HubMutableFeatureGate.Set("ManagedClusterAutoApproval=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable RawFeedbackJsonString feature gate
	err = features.SpokeMutableFeatureGate.Set(fmt.Sprintf("%s=true", ocmfeature.RawFeedbackJsonString))
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	ginkgo.By("starting the postgres instance")
	testPostgres, err = testpostgres.NewTestPostgres()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(testPostgres).ToNot(gomega.BeNil())

	maestroInstance = maestro.NewMaestro(testPostgres.Port)
	ginkgo.By("starting the maestro instance")
	maestroCtx, maestroCancel = context.WithCancel(context.Background())
	err = maestroInstance.Start(maestroCtx)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	openAPIClient = openapi.NewAPIClient(openapi.NewConfiguration())
	resourceService = maestroInstance.ResourceService()

	ginkgo.By("starting the grpc server")
	dbConfig := dbconfig.NewDatabaseConfig()
	dbConfig.Host = "localhost"
	dbConfig.Port = int(testPostgres.Port)
	dbConfig.Name = "maestro"
	dbConfig.Username = "postgres"
	dbConfig.Password = "postgres"
	stopGRPCServer = runGRPCServer("grpc-server", gRPCServerOptions, dbConfig, hubCfg)

	// wait for the grpc server to be ready
	gomega.Eventually(func() error {
		_, err := net.DialTimeout("tcp", "localhost:8090", 5*time.Second)
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	hubOption := hub.NewHubManagerOptions()
	hubOption.ImportOption.APIServerURL = hubCfg.Host
	hubOption.EnabledRegistrationDrivers = []string{operatorapiv1.GRPCAuthType, operatorapiv1.CSRAuthType}
	hubOption.GRPCCAFile = gRPCServerOptions.ClientCAFile
	hubOption.GRPCCAKeyFile = gRPCCAKeyFile

	stopHub = runHub("hub", hubOption, hubCfg)
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("stopping the hub")
	stopHub()

	ginkgo.By("stopping the grpc server")
	stopGRPCServer()

	ginkgo.By("stopping the maestro instance")
	maestroCancel()

	ginkgo.By("stopping the postgres instance")
	err := testPostgres.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.By("tearing down the test environment")
	err = testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	ginkgo.By("removing the test directory")
	err = os.RemoveAll(util.TestDir)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})

func runGRPCServer(name string, gRPCServerConfig *grpcserver.GRPCServerOptions, dbConfig *dbconfig.DatabaseConfig, cfg *rest.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	serverConfig := &grpc.GRPCServerConfig{
		GRPCConfig: gRPCServerConfig,
		DBConfig:   dbConfig,
	}
	serverConfigBytes, err := yaml.Marshal(serverConfig)
	if err != nil {
		klog.Fatalf("Failed to marshal grpc server config: %v", err)
	}
	serverConfigFile := path.Join(util.TestDir, "grpcserver", "server-config.yaml")
	if err := os.MkdirAll(path.Dir(serverConfigFile), 0755); err != nil {
		klog.Fatalf("Failed to create directory for grpc server config file %s: %v", serverConfigFile, err)
	}
	if err := os.WriteFile(serverConfigFile, serverConfigBytes, 0600); err != nil {
		klog.Fatalf("Failed to write grpc server config file %s: %v", serverConfigFile, err)
	}

	grpcServerOptions := grpc.NewGRPCServerOptions()
	grpcServerOptions.GRPCServerConfigFile = serverConfigFile

	runGRPCServerWithContext(ctx, name, grpcServerOptions, cfg)

	return cancel
}

func bootstrapRBAC(ctx context.Context, cfg *rest.Config) error {
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return err
	}
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-managedcluster-role",
		},
		Rules: []rbacv1.PolicyRule{
			// ManagedCluster
			{
				APIGroups: []string{"cluster.open-cluster-management.io"},
				Resources: []string{"managedclusters", "managedclusters/status", "managedclusters/finalizers"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			// CSR
			{
				APIGroups: []string{"certificates.k8s.io"},
				Resources: []string{"certificatesigningrequests"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
			},
			{
				APIGroups: []string{"certificates.k8s.io"},
				Resources: []string{"certificatesigningrequests/approval", "certificatesigningrequests/status"},
				Verbs:     []string{"update", "patch"},
			},
			// Leases (coordination.k8s.io)
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			// ManifestWorks (work.open-cluster-management.io)
			{
				APIGroups: []string{"work.open-cluster-management.io"},
				Resources: []string{"manifestworks", "manifestworks/status", "manifestworks/finalizers"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			// ManagedClusterAddons (addon.open-cluster-management.io)
			{
				APIGroups: []string{"addon.open-cluster-management.io"},
				Resources: []string{"managedclusteraddons", "managedclusteraddons/status", "managedclusteraddons/finalizers"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}
	if err := k8sClient.Create(ctx, clusterRole); err != nil {
		return err
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-managedcluster-binding",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				Name:     "test-client", // agent run as user "test-client"
				APIGroup: "rbac.authorization.k8s.io",
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "test-managedcluster-role",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
	if err := k8sClient.Create(ctx, clusterRoleBinding); err != nil {
		return err
	}

	return nil
}

func runGRPCServerWithContext(ctx context.Context, name string, grpcServerOptions *grpc.GRPCServerOptions, cfg *rest.Config) {
	go func() {
		err := grpcServerOptions.Run(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder(name),
		})
		if err != nil {
			klog.Fatalf("Failed to run grpc server %s: %v", name, err)
		}
	}()
}

func runHub(name string, opt *hub.HubManagerOptions, cfg *rest.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	runHubWithContext(ctx, name, opt, cfg)
	return cancel
}

func runHubWithContext(ctx context.Context, name string, opt *hub.HubManagerOptions, cfg *rest.Config) {
	go func() {
		err := opt.RunControllerManager(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder(name),
		})
		if err != nil {
			klog.Fatalf("Failed to run hub %s: %v", name, err)
		}
	}()
}

func runAgent(name string, opt *spoke.SpokeAgentOptions, commOption *commonoptions.AgentOptions, cfg *rest.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	agentConfig := spoke.NewSpokeAgentConfig(commOption, opt, cancel)
	runAgentWithContext(ctx, name, agentConfig, cfg)
	return cancel
}

func runAgentWithContext(ctx context.Context, name string, agentConfig *spoke.SpokeAgentConfig, cfg *rest.Config) {
	go func() {
		err := agentConfig.RunSpokeAgent(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder(name),
		})
		if err != nil {
			fmt.Printf("Failed to run agent %s: %v\n", name, err)
			return
		}
	}()
}

func runWorkAgent(name string, o *workspoke.WorkloadAgentOptions, commOption *commonoptions.AgentOptions, cfg *rest.Config) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	agentConfig := workspoke.NewWorkAgentConfig(commOption, o)
	runWorkAgentWithContext(ctx, name, agentConfig, cfg)
	return cancel
}

func runWorkAgentWithContext(ctx context.Context, name string, agentConfig *workspoke.WorkAgentConfig, cfg *rest.Config) {
	go func() {
		err := agentConfig.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder(name),
		})
		if err != nil {
			fmt.Printf("Failed to run work agent %s: %v\n", name, err)
			return
		}
	}()
}
