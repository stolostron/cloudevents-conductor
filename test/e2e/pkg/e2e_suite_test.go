package e2e

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-online/maestro/pkg/api/openapi"
	"github.com/openshift-online/maestro/pkg/client/cloudevents/grpcsource"
	"github.com/openshift-online/ocm-sdk-go/logging"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
)

var (
	cancel context.CancelFunc
	ctx    context.Context

	managedClusterName = "cluster1"

	// kubeconfigs
	hubKubeconfig     string
	managedKubeconfig string

	// maestro endpoints
	maestroAPIServerAddr  string
	maestroGRPCServerAddr string

	// maestro clients
	maestroAPIClient *openapi.APIClient
	sourceWorkClient workv1client.WorkV1Interface

	// kube clients
	hubClusterClient clusterclientset.Interface
	hubWorkClient    workclientset.Interface
	spokeKubeClient  kubernetes.Interface
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

func init() {
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")
	flag.StringVar(&maestroAPIServerAddr, "maestro-api-server", "", "Maestro Restful API server address")
	flag.StringVar(&maestroGRPCServerAddr, "maestro-grpc-server", "", "Maestro gRPC server address")
}

func TestE2E(t *testing.T) {
	OutputFail := func(message string, callerSkip ...int) {
		Fail(message, callerSkip...)
	}

	RegisterFailHandler(OutputFail)
	RunSpecs(t, "Cloudevents Conductor E2E Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	Expect(maestroAPIServerAddr).NotTo(BeEmpty(), "Maestro API server address must be provided")
	Expect(maestroGRPCServerAddr).NotTo(BeEmpty(), "Maestro gRPC server address must be provided")

	// initialize maestro api client
	cfg := &openapi.Configuration{
		DefaultHeader: make(map[string]string),
		UserAgent:     "OpenAPI-Generator/1.0.0/go",
		Debug:         false,
		Servers: openapi.ServerConfigurations{
			{
				URL:         maestroAPIServerAddr,
				Description: "current domain",
			},
		},
		OperationServers: map[string]openapi.ServerConfigurations{},
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				//nolint:gosec
				InsecureSkipVerify: true,
			}},
			Timeout: 10 * time.Second,
		},
	}

	maestroAPIClient = openapi.NewAPIClient(cfg)

	sourceGRPCOptions := &grpcoptions.GRPCOptions{
		Dialer: &grpcoptions.GRPCDialer{
			URL: maestroGRPCServerAddr,
			KeepAliveOptions: grpcoptions.KeepAliveOptions{
				Enable:  true,
				Time:    6 * time.Second,
				Timeout: 1 * time.Second,
			},
		},
	}

	var err error
	logger, err := logging.NewStdLoggerBuilder().Build()
	sourceWorkClient, err = grpcsource.NewMaestroGRPCSourceWorkClient(
		ctx,
		logger,
		maestroAPIClient,
		sourceGRPCOptions,
		fmt.Sprintf("source-work-client-%s", rand.String(5)),
	)
	Expect(err).NotTo(HaveOccurred())

	hubRestConfig, err := clientcmd.BuildConfigFromFlags("", hubKubeconfig)
	Expect(err).NotTo(HaveOccurred())

	hubClusterClient, err = clusterclientset.NewForConfig(hubRestConfig)
	Expect(err).NotTo(HaveOccurred())
	hubWorkClient, err = workclientset.NewForConfig(hubRestConfig)
	Expect(err).NotTo(HaveOccurred())

	spokeRestConfig, err := clientcmd.BuildConfigFromFlags("", managedKubeconfig)
	Expect(err).NotTo(HaveOccurred())
	spokeKubeClient, err = kubernetes.NewForConfig(spokeRestConfig)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	cancel()
})
