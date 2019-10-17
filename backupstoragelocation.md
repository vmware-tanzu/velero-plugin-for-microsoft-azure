# Velero Backup Storage Locations

## Backup Storage Location

Velero can store backups in a number of locations. These are represented in the cluster via the `BackupStorageLocation` CRD.

Velero must have at least one `BackupStorageLocation`. By default, this is expected to be named `default`, however the name can be changed by specifying `--default-backup-storage-location` on `velero server`.  Backups that do not explicitly specify a storage location will be saved to this `BackupStorageLocation`.

A sample YAML `BackupStorageLocation` looks like the following:

```yaml
apiVersion: velero.io/v1
kind: BackupStorageLocation
metadata:
  name: default
  namespace: velero
spec:
  provider: azure
  objectStorage:
    bucket: <YOUR_BLOB_CONTAINER> 
  config:
    resourceGroup: <YOUR_STORAGE_RESOURCE_GROUP>
    storageAccount: <YOUR_STORAGE_ACCOUNT>
```

### Parameter Reference

The configurable parameters are as follows:

#### Main config parameters

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `provider` | String `gcp` | Required Field | The name for the cloud provider which will be used to actually store the backups. |
| `objectStorage` | ObjectStorageLocation | Specification of the object storage for the given provider. |
| `objectStorage/bucket` | String | Required Field | The storage bucket where backups are to be uploaded. |
| `objectStorage/prefix` | String | Optional Field | The directory inside a storage bucket where backups are to be uploaded. |
| `accessMode` | String | `ReadWrite` | How Velero can access the backup storage location. Valid values are `ReadWrite`, `ReadOnly`. |

#### Azure specific

##### config

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `resourceGroup` | string | Required Field | Name of the resource group containing the storage account for this backup storage location. |
| `storageAccount` | string | Required Field | Name of the storage account for this backup storage location. |
| `subscriptionId` | string | Optional Field | ID of the subscription for this backup storage location. |