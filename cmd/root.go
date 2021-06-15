package cmd

import (
	"fmt"
	"github.com/danielxiao/migrator/pkg/client"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu/velero/pkg/cmd/server/plugin"
	"os"
)

var rootCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate workloads from one Kubernetes cluster to another",
	Long: "Migrate workloads from one Kubernetes cluster to another",
}

var pluginsKubeConfig string

func init() {
	//This is hacking to get the kubeconfig file
	pluginsKubeConfig = os.Getenv("PLUGINS_KUBECONFIG")
	if pluginsKubeConfig == "" {
		fmt.Println("PLUGINS_KUBECONFIG is not set")
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "run-plugins" {
		rootCmd.AddCommand(plugin.NewCommand(client.NewFactory("run-plugins", pluginsKubeConfig)))
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}