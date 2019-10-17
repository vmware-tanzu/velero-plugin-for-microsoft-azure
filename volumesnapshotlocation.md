# Velero Volume Snapshot Location

## Volume Snapshot Location

A volume snapshot location is the location in which to store the volume snapshots created for a backup.

Velero can be configured to take snapshots of volumes from multiple providers. Velero also allows you to configure multiple possible `VolumeSnapshotLocation` per provider, although you can only select one location per provider at backup time.

Each VolumeSnapshotLocation describes a provider + location. These are represented in the cluster via the `VolumeSnapshotLocation` CRD. Velero must have at least one `VolumeSnapshotLocation` per cloud provider.

A sample YAML `VolumeSnapshotLocation` looks like the following:

```yaml
apiVersion: velero.io/v1
kind: VolumeSnapshotLocation
metadata:
  name: gcp-default
  namespace: velero
spec:
  provider: azure
  config:
    apiTimeout: <YOUR_TIMEOUT>
```

### Parameter Reference

The configurable parameters are as follows:

#### Main config parameters

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `provider` | String `azure` | Required Field | The name for the cloud provider that will be used to actually store the volume |
| `config` | | | See the corresponding Azure specific config below.

#### Azure specific

##### config

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `apiTimeout` | metav1.Duration | 2m0s | How long to wait for an Azure API request to complete before timeout. |
| `resourceGroup` | string | Optional | The name of the resource group where volume snapshots should be stored, if different from the cluster's resource group. |
| `subscriptionId` | string | Optional | The ID of the subscription where volume snapshots should be stored, if different from the cluster's subscription. Requires `resourceGroup`to be set.
