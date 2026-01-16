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
    # Required if using a storage account access key to authenticate rather than a service principal.
    storageAccountKeyEnvVar: MY_BACKUP_STORAGE_ACCOUNT_KEY_ENV_VAR

    # ID of the subscription for this backup storage location.
    #
    # Optional.
    subscriptionId: my-subscription

    # URI of the blob endpoint of the storage account.
    #
    # Optional. This will ensure that velero uses the provided URI to communicate to the Storage Account,
    # and it will not try to fetch the Endpoint by making an ARM call.
    # If this field is provided then resourceGroup, subscriptionId can be left empty
    storageAccountURI: https://my-sa.blob.core.windows.net

    # Boolean parameter to decide whether to use AAD for authenticating with the storage account.
    # If false/ not provided, plugin will fallback to using ListKeys
    #
    # Optional. Recommended.
    useAAD: "true"

    # URI of the AAD endpoint of the storage account.
    #
    # Note that useAAD: should be set to "true" in order to use the provided AAD URI and http(s):// scheme is required to authenticate
    #
    # Optional. This will ensure that velero uses the provided AAD URI to authenticate to the Storage Account.
    activeDirectoryAuthorityURI: https://login.microsoftonline.us/

    # The block size, in bytes, to use when uploading objects to Azure blob storage.
    # See https://docs.microsoft.com/en-us/rest/api/storageservices/understanding-block-blobs--append-blobs--and-page-blobs#about-block-blobs
    # for more information on block blobs.
    #
    # Optional (defaults to 1048576, i.e. 1MB, maximum 104857600, i.e. 100MB).
    blockSizeInBytes: "1048576"

    # APIVersion to use with blob storage API requests.
    #
    # Optional. This is used for private cloud environments which may not have the same available api versions as
    # the public cloud
    apiVersion: ""
```
