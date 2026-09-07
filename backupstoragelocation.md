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

    # Key name in the BSL credential file that holds a container-scoped SAS token.
    # When set, all other authentication methods (shared key, AAD) are bypassed
    # and the plugin authenticates using the SAS token alone.
    # Cannot be combined with useAAD: "true".
    #
    # Use this when only a container-scoped SAS token is available and no account
    # key or service principal can be provided.
    #
    # The SAS token may optionally include a leading "?" which is stripped automatically.
    #
    # Optional. When set, storageAccountBlobEndpoint is required.
    storageAccountSASTokenEnvVar: AZURE_STORAGE_ACCOUNT_ACCESS_KEY

    # Blob service endpoint URL for the storage account.
    # Required when using storageAccountSASTokenEnvVar.
    #
    # Set this to the blob service root URL for the storage account, for example:
    #   storageAccountBlobEndpoint: "https://myaccount.blob.core.windows.net/"
    #   storageAccountBlobEndpoint: "https://myaccount.z17.blob.storage.azure.net/"
    #
    # Optional.
    storageAccountBlobEndpoint: https://my-account.z17.blob.storage.azure.net/
```

## Using SAS token authentication

When only a container-scoped SAS token is available (no account key or service principal), use the following minimal BSL configuration:

```yaml
apiVersion: velero.io/v1
kind: BackupStorageLocation
metadata:
  name: default
  namespace: velero
spec:
  provider: velero.io/azure
  objectStorage:
    bucket: my-container
  config:
    storageAccount: my-storage-account
    storageAccountSASTokenEnvVar: AZURE_STORAGE_ACCOUNT_ACCESS_KEY
    storageAccountBlobEndpoint: https://my-storage-account.blob.core.windows.net/
  credential:
    name: velero-azure-credentials
    key: cloud
```

The SAS token must be stored in a Kubernetes Secret in `KEY=VALUE` format, referenced by `spec.credential` on the BSL:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: velero-azure-credentials
  namespace: velero
type: Opaque
stringData:
  cloud: |
    AZURE_STORAGE_ACCOUNT_ACCESS_KEY=<sas-token>
```

The value of `storageAccountSASTokenEnvVar` (`AZURE_STORAGE_ACCOUNT_ACCESS_KEY` above) is the key name looked up in the credential file. The token value may include or omit the leading `?` — both formats are accepted.
