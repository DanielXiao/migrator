package migrate

import (
	"github.com/danielxiao/migrator/pkg/client"
	"github.com/sirupsen/logrus"
	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/backup"
	"github.com/vmware-tanzu/velero/pkg/builder"
	veleroclient "github.com/vmware-tanzu/velero/pkg/client"
	velerodiscovery "github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/plugin/clientmgmt"
	"github.com/vmware-tanzu/velero/pkg/podexec"
	"github.com/vmware-tanzu/velero/pkg/restore"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"
)

var nonRestorableResources = []string{
	"nodes",
	"events",
	"events.events.k8s.io",
}

var defaultRestorePriorities = []string{
	"customresourcedefinitions",
	"namespaces",
	"storageclasses",
	"volumesnapshotclass.snapshot.storage.k8s.io",
	"volumesnapshotcontents.snapshot.storage.k8s.io",
	"volumesnapshots.snapshot.storage.k8s.io",
	"persistentvolumes",
	"persistentvolumeclaims",
	"secrets",
	"configmaps",
	"serviceaccounts",
	"limitranges",
	"pods",
	"replicasets.apps",
	"clusters.cluster.x-k8s.io",
	"clusterresourcesets.addons.cluster.x-k8s.io",
}

func runBackup(logger *logrus.Logger, kubeConfig string, plugins string, backupFile io.Writer, namespaces []string) (*velerov1api.Backup, error) {
	f := client.NewFactory("source-cluster", kubeConfig)
	kubeClient, err := f.KubeClient()
	if err != nil {
		return nil, err
	}

	veleroClient, err := f.Client()
	if err != nil {
		return nil, err
	}

	dynamicClient, err := f.DynamicClient()
	if err != nil {
		return nil, err
	}

	kubeClientConfig, err := f.ClientConfig()
	if err != nil {
		return nil, err
	}

	//Register internal and custom plugins during process start
	pluginRegistry := clientmgmt.NewRegistry(plugins, logger, logger.Level)
	if err := pluginRegistry.DiscoverPlugins(); err != nil {
		return nil, err
	}

	//Get backup plugin instances
	pluginManager := clientmgmt.NewManager(logger, logger.Level, pluginRegistry)
	defer pluginManager.CleanupClients()
	actions, err := pluginManager.GetBackupItemActions()
	if err != nil {
		return nil, err
	}

	//Initialize discovery helper
	discoveryHelper, err := velerodiscovery.NewHelper(veleroClient.Discovery(), logger)
	if err != nil {
		return nil, err
	}

	//Intitialize kubernetesBackupper
	k8sBackupper, err := backup.NewKubernetesBackupper(
		veleroClient.VeleroV1(),
		discoveryHelper,
		veleroclient.NewDynamicFactory(dynamicClient),
		podexec.NewPodCommandExecutor(kubeClientConfig, kubeClient.CoreV1().RESTClient()),
		nil,
		0,
		false,
	)
	if err != nil {
		return nil, err
	}

	//Run the backup
	backupParams := builder.ForBackup("default", "source").IncludedNamespaces(namespaces...).ExcludedResources("persistentvolumeclaims", "persistentvolumes").DefaultVolumesToRestic(false).Result()
	backupReq := backup.Request{
		Backup: backupParams,
	}
	if err = k8sBackupper.Backup(logger, &backupReq, backupFile, actions, pluginManager); err != nil {
		return nil, err
	}
	return backupParams, nil
}

func runRestore(logger *logrus.Logger, kubeConfig string, plugins string, backupFile io.Reader, backup *velerov1api.Backup) (*restore.Result, *restore.Result, error) {
	f := client.NewFactory("dest-cluster", kubeConfig)
	kubeClient, err := f.KubeClient()
	if err != nil {
		return nil, nil, err
	}

	veleroClient, err := f.Client()
	if err != nil {
		return nil, nil, err
	}

	dynamicClient, err := f.DynamicClient()
	if err != nil {
		return nil, nil, err
	}

	kubeClientConfig, err := f.ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	//Register internal and custom plugins during process start
	pluginRegistry := clientmgmt.NewRegistry(plugins, logger, logger.Level)
	if err := pluginRegistry.DiscoverPlugins(); err != nil {
		return nil, nil, err
	}

	//Get restore plugin instances
	pluginManager := clientmgmt.NewManager(logger, logger.Level, pluginRegistry)
	defer pluginManager.CleanupClients()
	actions, err := pluginManager.GetRestoreItemActions()
	if err != nil {
		return nil, nil, err
	}

	//Initialize discovery helper
	discoveryHelper, err := velerodiscovery.NewHelper(veleroClient.Discovery(), logger)
	if err != nil {
		return nil, nil, err
	}

	//Intitialize KubernetesRestorer
	k8sRestorer, err := restore.NewKubernetesRestorer(
		veleroClient.VeleroV1(),
		discoveryHelper,
		veleroclient.NewDynamicFactory(dynamicClient),
		defaultRestorePriorities,
		kubeClient.CoreV1().Namespaces(),
		nil,
		240 * time.Minute,
		10 * time.Minute,
		logger,
		podexec.NewPodCommandExecutor(kubeClientConfig, kubeClient.CoreV1().RESTClient()),
		kubeClient.CoreV1().RESTClient(),
	)
	if err != nil {
		return nil, nil, err
	}

	//Run the restore
	restoreParams := builder.ForRestore("default", "dest").ExcludedResources(nonRestorableResources...).Backup(backup.Name).RestorePVs(false).Result()
	restoreReq := restore.Request{
		Log:              logger,
		Restore:          restoreParams,
		Backup:           backup,
		BackupReader:     backupFile,
	}
	restoreWarnings, restoreErrors := k8sRestorer.Restore(restoreReq, actions, nil, pluginManager)
	return &restoreWarnings, &restoreErrors, nil
}

func copyKubeConfig(from, to string) error {
	fromFile, err := os.Open(from)
	if err != nil {
		return err
	}
	defer fromFile.Close()
	if _, err = os.Stat(path.Dir(to)); os.IsNotExist(err) {
		os.MkdirAll(path.Dir(to), 0755)
	}
	if _, err = os.Stat(to); err == nil {
		if err = os.Remove(to); err != nil {
			return err
		}
	}
	toFile, err := os.Create(to)
	if err != nil {
		return err
	}
	defer toFile.Close()
	_, err = io.Copy(toFile, fromFile)
	return err
}

func Run(logger *logrus.Logger, sourceKubeConfig, destKubeConfig, pluginKubeConfig, plugins, cache string, namespaces []string) {
	//Create the backup file
	backupFile, err := ioutil.TempFile(cache, "backup-*.tar")
	if err != nil {
		logger.WithError(err).Error("Failed to create a file for saving k8s artifacts")
		return
	}
	defer backupFile.Close()
	logger.Infof("Created %s for saving k8s artifacts", backupFile.Name())
	//Copy source kubeconfig to plugins kubeconfig
	if err := copyKubeConfig(sourceKubeConfig, pluginKubeConfig); err != nil {
		logger.WithError(err).Error("Failed to copy kubeconfig for velero plugins")
	}
	//Run backup
	backup, err := runBackup(logger, sourceKubeConfig, plugins, backupFile, namespaces)
	if err != nil {
		logger.WithError(err).Error("Failed to run backup against the source cluster")
		return
	}
	logger.Info("Run backup successfully against the source cluster")
	//Copy dest kubeconfig to plugins kubeconfig
	if err := copyKubeConfig(destKubeConfig, pluginKubeConfig); err != nil {
		logger.WithError(err).Error("Failed to copy kubeconfig for velero plugins")
		return
	}
	//Run restore
	if _, err = backupFile.Seek(0, 0); err != nil {
		logger.WithError(err).Errorf("Failed to set offset for restore on %s", backupFile.Name())
		return
	}
	restoreWarnings, restoreErrors, err := runRestore(logger, destKubeConfig, plugins, backupFile, backup)
	if err != nil {
		logger.WithError(err).Error("Failed to run restore against the destination cluster")
		return
	}
	if len(restoreWarnings.Namespaces) > 0 || len(restoreWarnings.Velero) > 0 || len(restoreWarnings.Cluster) > 0  {
		logger.Warning(restoreWarnings)
	}
	if len(restoreErrors.Namespaces) > 0 || len(restoreErrors.Velero) > 0 || len(restoreErrors.Cluster) > 0  {
		logger.Error(restoreErrors)
	} else {
		logger.Info("Run restore successfully against the destination cluster")
	}
	os.Remove(backupFile.Name())
	logger.Infof("Removed artifacts %s", backupFile.Name())
}
