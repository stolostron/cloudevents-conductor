package integration

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/api/openapi"
	"github.com/openshift-online/maestro/pkg/services"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/stolostron/cloudevents-conductor/test/integration/testpostgres"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1 "open-cluster-management.io/api/work/v1"

	"github.com/stolostron/cloudevents-conductor/test/integration/maestro"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/registration/spoke/addon"
	"open-cluster-management.io/ocm/pkg/registration/spoke/registration"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var hubCfg, spokeCfg *rest.Config
var bootstrapKubeConfigFile string

var testEnv *envtest.Environment
var securePort string
var serverCertFile string

var hubKubeClient kubernetes.Interface
var hubClusterClient clusterclientset.Interface
var hubWorkClient workclientset.Interface

var spokeKubeClient kubernetes.Interface
var spokeWorkClient workclientset.Interface

var testNamespace string

var authn *util.TestAuthn

var stopHub context.CancelFunc

var startHub func(m *hub.HubManagerOptions)
var hubOption *hub.HubManagerOptions

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

var testPostgres *testpostgres.TestPostgres

var maestroInstance *maestro.Maestro
var maestroCtx context.Context
var maestroCancel context.CancelFunc
var openAPIClient *openapi.APIClient
var resources services.ResourceService

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

	// crank up the addon lease sync and udpate speed
	spoke.AddOnLeaseControllerSyncInterval = 5 * time.Second
	addon.AddOnLeaseControllerLeaseDurationSeconds = 1

	// install cluster CRD and start a local kube-apiserver
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

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

	spokeWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(spokeWorkClient).ToNot(gomega.BeNil())

	// prepare test namespace
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		testNamespace = "open-cluster-management-agent"
	} else {
		testNamespace = string(nsBytes)
	}
	err = util.PrepareSpokeAgentNamespace(hubKubeClient, testNamespace)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable DefaultClusterSet feature gate
	err = features.HubMutableFeatureGate.Set("DefaultClusterSet=true")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable ManagedClusterAutoApproval feature gate
	err = features.HubMutableFeatureGate.Set("ManagedClusterAutoApproval=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// context
	var ctx context.Context
	// hub start function
	startHub = func(m *hub.HubManagerOptions) {
		ctx, stopHub = context.WithCancel(context.Background())
		go func() {
			defer ginkgo.GinkgoRecover()
			m.ImportOption.APIServerURL = cfg.Host
			err := m.RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
	}

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
	resources = maestroInstance.ResourceService()
})

var _ = ginkgo.AfterSuite(func() {
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
