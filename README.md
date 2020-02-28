# Velero plugins for Microsoft Azure

## Overview

This repository contains these plugins to support running Velero on Microsoft Azure:

- An object store plugin for persisting and retrieving backups on Azure Blob Storage. Content of backup is log files, warning/error files, restore logs.

- A volume snapshotter plugin for creating snapshots from volumes (during a backup) and volumes from snapshots (during a restore) on Azure Managed Disks.

## Compatibility

Below is a listing of plugin versions and respective Velero versions that are compatible.

| Plugin Version  | Velero Version |
|-----------------|----------------|
| v1.0.x          | v1.2.0         |

## Setup

To set up Velero on Azure, you:

- [Create an Azure storage account and blob container][1]
- [Set permissions for Velero][2]
- [Install and start Velero][3]

If you do not have the `az` Azure CLI 2.0 installed locally, follow the [install guide][18] to set it up.

Run:

```bash
az login
```

## Setup Azure storage account and blob container

### (Optional) Change to the Azure subscription you want to create your backups in

By default, Velero will store backups in the same Subscription as your VMs and disks and will
not allow you to restore backups to a Resource Group in a different Subscription. To enable backups/restore
across Subscriptions you will need to specify the Subscription ID to backup to.

Use `az` to switch to the Subscription the backups should be created in.

First, find the Subscription ID by name.

```bash
AZURE_BACKUP_SUBSCRIPTION_NAME=<NAME_OF_TARGET_SUBSCRIPTION>
AZURE_BACKUP_SUBSCRIPTION_ID=$(az account list --query="[?name=='$AZURE_BACKUP_SUBSCRIPTION_NAME'].id | [0]" -o tsv)
```

Second, change the Subscription.

```bash
az account set -s $AZURE_BACKUP_SUBSCRIPTION_ID
```

Execute the next step – creating an storage account and blob container – using the active Subscription.

### Create Azure storage account and blob container

Velero requires a storage account and blob container in which to store backups.

The storage account can be created in the same Resource Group as your Kubernetes cluster or
separated into its own Resource Group. The example below shows the storage account created in a
separate `Velero_Backups` Resource Group.

The storage account needs to be created with a globally unique id since this is used for dns. In
the sample script below, we're generating a random name using `uuidgen`, but you can come up with
this name however you'd like, following the [Azure naming rules for storage accounts][19]. The
storage account is created with encryption at rest capabilities (Microsoft managed keys) and is
configured to only allow access via https.

Create a resource group for the backups storage account. Change the location as needed.

```bash
AZURE_BACKUP_RESOURCE_GROUP=Velero_Backups
az group create -n $AZURE_BACKUP_RESOURCE_GROUP --location WestUS
```

Create the storage account.

```bash
AZURE_STORAGE_ACCOUNT_ID="velero$(uuidgen | cut -d '-' -f5 | tr '[A-Z]' '[a-z]')"
az storage account create \
    --name $AZURE_STORAGE_ACCOUNT_ID \
    --resource-group $AZURE_BACKUP_RESOURCE_GROUP \
    --sku Standard_GRS \
    --encryption-services blob \
    --https-only true \
    --kind BlobStorage \
    --access-tier Hot
```

Create the blob container named `velero`. Feel free to use a different name, preferably unique to a single Kubernetes cluster. See the [FAQ][11] for more details.

```bash
BLOB_CONTAINER=velero
az storage container create -n $BLOB_CONTAINER --public-access off --account-name $AZURE_STORAGE_ACCOUNT_ID
```

## Set permissions for Velero

### Kubernetes cluster prerequisites

Ensure that the VMs for your agent pool allow Managed Disks. If I/O performance is critical,
consider using Premium Managed Disks, which are SSD backed.

### Get resource group for persistent volume snapshots

_(Optional) If you decided to backup to a different Subscription, make sure you change back to the Subscription
of your cluster's resources before continuing._

1. Set the name of the Resource Group that contains your Kubernetes cluster's virtual machines/disks.

    **WARNING**: If you're using [AKS][22], `AZURE_RESOURCE_GROUP` must be set to the name of the auto-generated resource group that is created
    when you provision your cluster in Azure, since this is the resource group that contains your cluster's virtual machines/disks.

    ```bash
    AZURE_RESOURCE_GROUP=<NAME_OF_RESOURCE_GROUP>
    ```

    If you are unsure of the Resource Group name, run the following command to get a list that you can select from. Then set the `AZURE_RESOURCE_GROUP` environment variable to the appropriate value.

    ```bash
    az group list --query '[].{ ResourceGroup: name, Location:location }'
    ```

    Get your cluster's Resource Group name from the `ResourceGroup` value in the response, and use it to set `$AZURE_RESOURCE_GROUP`.

### Create service principal

To integrate Velero with Azure, you must create a Velero-specific [service principal][17].

1. Obtain your Azure Account Subscription ID and Tenant ID:

    ```bash
    AZURE_SUBSCRIPTION_ID=`az account list --query '[?isDefault].id' -o tsv`
    AZURE_TENANT_ID=`az account list --query '[?isDefault].tenantId' -o tsv`
    ```

1. Create a service principal with `Contributor` role. This will have subscription-wide access, so protect this credential.

    If you'll be using Velero to backup multiple clusters with multiple blob containers, it may be desirable to create a unique username per cluster rather than the default `velero`.

    Create service principal and let the CLI generate a password for you. Make sure to capture the password.

    _(Optional) If you are using a different Subscription for backups and cluster resources, make sure to specify both subscriptions
    in the `az` command using `--scopes`._

    ```bash
    AZURE_CLIENT_SECRET=`az ad sp create-for-rbac --name "velero" --role "Contributor" --query 'password' -o tsv \
      --scopes  /subscriptions/$AZURE_SUBSCRIPTION_ID[ /subscriptions/$AZURE_BACKUP_SUBSCRIPTION_ID]`
    ```

    NOTE: Ensure that value for `--name` does not conflict with other service principals/app registrations.

    After creating the service principal, obtain the client id.

    ```bash
    AZURE_CLIENT_ID=`az ad sp list --display-name "velero" --query '[0].appId' -o tsv`
    ```

1. Now you need to create a file that contains all the environment variables you just set. The command looks like the following:

```bash
    cat << EOF  > ./credentials-velero
    AZURE_SUBSCRIPTION_ID=${AZURE_SUBSCRIPTION_ID}
    AZURE_TENANT_ID=${AZURE_TENANT_ID}
    AZURE_CLIENT_ID=${AZURE_CLIENT_ID}
    AZURE_CLIENT_SECRET=${AZURE_CLIENT_SECRET}
    AZURE_RESOURCE_GROUP=${AZURE_RESOURCE_GROUP}
    AZURE_CLOUD_NAME=AzurePublicCloud
    EOF
```

> available `AZURE_CLOUD_NAME` values: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`, `AzureGermanCloud`

## Install and start Velero

[Download][4] Velero

Install Velero, including all prerequisites, into the cluster and start the deployment. This will create a namespace called `velero`, and place a deployment named `velero` in it.

```bash
velero install \
    --provider azure \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.0.1 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_ID[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
    --snapshot-location-config apiTimeout=<YOUR_TIMEOUT>[,resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID]
```

Additionally, you can specify `--use-restic` to enable restic support, and `--wait` to wait for the deployment to be ready.

(Optional) Specify [additional configurable parameters][7] for the `--backup-location-config` flag.

(Optional) Specify [additional configurable parameters][8] for the `--snapshot-location-config` flag.

(Optional) Specify [CPU and memory resource requests and limits][9] for the Velero/restic pods.

For more complex installation needs, use either the Helm chart, or add `--dry-run -o yaml` options for generating the YAML representation for the installation.

[1]: #Create-Azure-storage-account-and-blob-container
[2]: #Set-permissions-for-Velero
[3]: #Install-and-start-Velero
[4]: https://velero.io/docs/master/install-overview/#install-the-cli
[7]: backupstoragelocation.md
[8]: volumesnapshotlocation.md
[11]: https://velero.io/docs/master/faq/
[17]: https://docs.microsoft.com/en-us/azure/active-directory/develop/active-directory-application-objects
[18]: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli
[19]: https://docs.microsoft.com/en-us/azure/architecture/best-practices/naming-conventions#storage
[9]: https://velero.io/docs/master/install-requirements
[22]: https://azure.microsoft.com/en-us/services/kubernetes-service/
