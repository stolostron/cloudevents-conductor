package main

import (
	"context"
	goflag "flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/utils/clock"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/version"

	"github.com/stolostron/cloudevents-conductor/pkg/server/grpc"
)

// main is the entry point for the CloudEvents Conductor application.
func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.AddFlags(pflag.CommandLine)
	logs.InitLogs()
	defer logs.FlushLogs()

	command := &cobra.Command{
		Use:   "CloudEvents Conductor",
		Short: "The CloudEvents Conductor is deployed on the hub and exposed to allow connections from managed clusters.",
	}

	command.AddCommand(newGRPCCommand())

	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1) //nolint:gocritic
	}
}

func newGRPCCommand() *cobra.Command {
	opts := commonoptions.NewOptions()
	grpcServerOpts := grpc.NewGRPCServerOptions()

	cmdConfig := opts.NewControllerCommandConfig("grpc-server", version.Get(), grpcServerOpts.Run, clock.RealClock{})

	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "grpc"
	cmd.Short = "Start the gRPC Server"

	flags := cmd.Flags()
	opts.AddFlags(flags)
	grpcServerOpts.AddFlags(flags)

	return cmd
}
