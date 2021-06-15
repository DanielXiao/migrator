package cmd

import (
	"github.com/danielxiao/migrator/pkg/migrate"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu/velero/pkg/util/logging"
	"log"
	"os"
	"strings"
)

var sourceKubeConfig, destKubeConfig, namespaces, plugins, cache string

var execCmd = &cobra.Command{
	Use: "exec",
	Short: "Execute workload migration",
	Long: "Execute workload migration",
	Example: "migrate exec -s /Users/yifengx/kubeconfig/tkgi-1 -d /Users/yifengx/kubeconfig/tkgi-2 -n webapp -p /Users/yifengx/empty -c /Users/yifengx/mig",
	Run: func(cmd *cobra.Command, args []string) {
		// go-plugin uses log.Println to log when it's waiting for all plugin processes to complete so we need to
		// set its output to stdout.
		log.SetOutput(os.Stdout)
		// Make sure we log to stdout so cloud log dashboards don't show this as an error.
		logrus.SetOutput(os.Stdout)
		// Velero's DefaultLogger logs to stdout, so all is good there.
		logLevel := logging.LogLevelFlag(logrus.DebugLevel).Parse()
		format := logging.NewFormatFlag().Parse()
		logger := logging.DefaultLogger(logLevel, format)
		migrate.Run(logger, sourceKubeConfig, destKubeConfig, pluginsKubeConfig, plugins, cache, strings.Split(namespaces, ","))
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
	execCmd.Flags().StringVarP(&sourceKubeConfig, "source", "s", "", "kubeconfig file path of the source cluster")
	execCmd.Flags().StringVarP(&destKubeConfig, "destination", "d", "", "kubeconfig file path of the destination cluster")
	execCmd.Flags().StringVarP(&namespaces, "namespaces", "n", "", "namespaces to migrate, separated by ,")
	execCmd.Flags().StringVarP(&plugins, "plugins", "p", "", "Velero custom plugins path")
	execCmd.Flags().StringVarP(&cache, "cache", "c", "", "Cache directory for Kubernetes artifacts")
}