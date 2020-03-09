# Backup Storage Location

The following sample Azure `BackupStorageLocation` YAML shows all of the configurable parameters. The items under `spec.config` can be provided as key-value pairs to the `velero install` command's `--backup-location-config` flag -- for example, `resourceGroup=my-rg,storageAccount=my-sa,...`.

```yaml
apiVersion: velero.io/v1
kind: BackupStorageLocation
metadata:
  name: default
  namespace: velero
spec:
  # Name of the object store plugin to use to connect to this location.
  #
  # Required.
  provider: velero.io/azure

  objectStorage:
    # The bucket/blob container in which to store backups.
    #
    # Required.
    bucket: my-bucket

    # The prefix within the bucket under which to store backups.
    #
    # Optional.
    prefix: my-prefix

  config:
    # Name of the resource group containing the storage account for this backup storage location.
    #
    # Required.
    resourceGroup: my-backup-resource-group

    # Name of the storage account for this backup storage location.
    #
    # Required.
    storageAccount: my-backup-storage-account

    # Name of the environment variable in $AZURE_CREDENTIALS_FILE that contains storage account key for this backup storage location.
    #
    # Optional.
    storageAccountKeyEnvVar: MY_BACKUP_STORAGE_ACCOUNT_KEY

    # ID of the subscription for this backup storage location.
    #
    # Optional.
    subscriptionId: my-subscription
```
