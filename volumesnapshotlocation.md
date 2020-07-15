# Volume Snapshot Location

The following sample Azure `VolumeSnapshotLocation` YAML shows all of the configurable parameters. The items under `spec.config` can be provided as key-value pairs to the `velero install` command's `--snapshot-location-config` flag -- for example, `apiTimeout=5m,resourceGroup=my-rg,...`.

```yaml
apiVersion: velero.io/v1
kind: VolumeSnapshotLocation
metadata:
  name: azure-default
  namespace: velero
spec:
  # Name of the volume snapshotter plugin to use to connect to this location.
  #
  # Required.
  provider: velero.io/azure

  config:
    # How long to wait for an Azure API request to complete before timeout.
    #
    # Optional (defaults to 2m0s).
    apiTimeout: 5m

    # The name of the resource group where volume snapshots should be stored, if different
    # from the cluster's resource group.
    #
    # Optional.
    resourceGroup: my-rg

    # The ID of the subscription where volume snapshots should be stored, if different
    # from the cluster's subscription. Requires "resourceGroup" to also be set.
    #
    # Optional.
    subscriptionId: alt-subscription

    # Azure offers the option to take full or incremental snapshots of managed disks.
    # - Set this parameter to true, to take incremental snapshots.
    # - If the parameter is omitted or set to false, full snapshots are taken (default).
    #
    # Optional.
    incremental: "<false|true>"
```
