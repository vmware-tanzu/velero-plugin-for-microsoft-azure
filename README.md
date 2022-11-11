
[![Build Status][101]][102]

# Velero plugins for Microsoft Azure

## Overview

This repository contains these plugins to support running Velero on Microsoft Azure:

- An object store plugin for persisting and retrieving backups on Azure Blob Storage. Content of backup is log files, warning/error files, restore logs.

- A volume snapshotter plugin for creating snapshots from volumes (during a backup) and volumes from snapshots (during a restore) on Azure Managed Disks.
  - Since v1.4.0 the snapshotter plugin can handle the volumes provisioned by CSI driver `disk.csi.azure.com`
  - Since v1.5.0 the snapshotter plugin can handle the zone-redundant storage(ZRS) managed disks which can be used to support backup/restore across different available zones.

## Compatibility

Below is a listing of plugin versions and respective Velero versions that are compatible.

| Plugin Version | Velero Version |
|----------------|----------------|
| v1.6.x         | v1.10.x        |
| v1.5.x         | v1.9.x         |
| v1.4.x         | v1.8.x         |
| v1.3.x         | v1.7.x         |
| v1.2.x         | v1.6.x         |
| v1.1.x         | v1.5.x         |
| v1.1.x         | v1.4.x         |
| v1.0.x         | v1.3.x         |
| v1.0.x         | v1.2.0         |


## Filing issues

If you would like to file a GitHub issue for the plugin, please open the issue on the [core Velero repo][103]


## Kubernetes cluster prerequisites

Ensure that the VMs for your agent pool allow Managed Disks. If I/O performance is critical,
consider using Premium Managed Disks, which are SSD backed.

## Setup

To set up Velero on Azure, you:

- [Create an Azure storage account and blob container][1]
- [Get the resource group containing your VMs and disks][4]
- [Set permissions for Velero][2]
- [Install and start Velero][3]


You can also use this plugin to create an additional [Backup Storage Location][12].

If you do not have the `az` Azure CLI 2.0 installed locally, follow the [install guide][21] to set it up.

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
this name however you'd like, following the [Azure naming rules for storage accounts][22]. The
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

## Get resource group containing your VMs and disks

_(Optional) If you decided to backup to a different Subscription, make sure you change back to the Subscription
of your cluster's resources before continuing._

1. Set the name of the Resource Group that contains your Kubernetes cluster's virtual machines/disks.

    **WARNING**: If you're using [AKS][25], `AZURE_RESOURCE_GROUP` must be set to the name of the auto-generated resource group that is created
    when you provision your cluster in Azure, since this is the resource group that contains your cluster's virtual machines/disks.

    ```bash
    AZURE_RESOURCE_GROUP=<NAME_OF_RESOURCE_GROUP>
    ```

    If you are unsure of the Resource Group name, run the following command to get a list that you can select from. Then set the `AZURE_RESOURCE_GROUP` environment variable to the appropriate value.

    ```bash
    az group list --query '[].{ ResourceGroup: name, Location:location }'
    ```

    Get your cluster's Resource Group name from the `ResourceGroup` value in the response, and use it to set `$AZURE_RESOURCE_GROUP`.

## Set permissions for Velero

There are several ways Velero can authenticate to Azure: (1) by using a Velero-specific [service principal][20]; (2) by using [AAD Pod Identity][23]; or (3) by using a storage account access key.

If you plan to use Velero to take Azure snapshots of your persistent volume managed disks, you **must** use the service principal or AAD Pod Identity method.

If you don't plan to take Azure disk snapshots, any method is valid.

### Specify Role
_**Note**: This is only required for (1) by using a Velero-specific service principal and  (2) by using ADD Pod Identity._  

1. Obtain your Azure Account Subscription ID:
   ```
   AZURE_SUBSCRIPTION_ID=`az account list --query '[?isDefault].id' -o tsv`
   ```

2. Specify the role  
There are two ways to specify the role: use the built-in role or create a custom one.  
   You can use the Azure built-in role `Contributor`:
   ```
   AZURE_ROLE=Contributor
   ```
   This will have subscription-wide access, so protect the credential generated with this role.
   
   It is always best practice to assign the minimum required permissions necessary for an application to do its work.  
   
   Here are the minimum required permissions needed by Velero to perform backups, restores, and deletions:
   - Storage Account
      - Microsoft.Storage/storageAccounts/listkeys/action 
      - Microsoft.Storage/storageAccounts/regeneratekey/action  
   - Disk Management
      - Microsoft.Compute/disks/read
      - Microsoft.Compute/disks/write
      - Microsoft.Compute/disks/endGetAccess/action
      - Microsoft.Compute/disks/beginGetAccess/action
   - Snapshot Management
      - Microsoft.Compute/snapshots/read
      - Microsoft.Compute/snapshots/write
      - Microsoft.Compute/snapshots/delete
      - Microsoft.Compute/disks/beginGetAccess/action
      - Microsoft.Compute/disks/endGetAccess/action
   
   Use the following commands to create a custom role which has the minimum required permissions:
   ```
   AZURE_ROLE=Velero
   az role definition create --role-definition '{
      "Name": "'$AZURE_ROLE'",
      "Description": "Velero related permissions to perform backups, restores and deletions",
      "Actions": [
          "Microsoft.Compute/disks/read",
          "Microsoft.Compute/disks/write",
          "Microsoft.Compute/disks/endGetAccess/action",
          "Microsoft.Compute/disks/beginGetAccess/action",
          "Microsoft.Compute/snapshots/read",
          "Microsoft.Compute/snapshots/write",
          "Microsoft.Compute/snapshots/delete",
          "Microsoft.Storage/storageAccounts/listkeys/action",
          "Microsoft.Storage/storageAccounts/regeneratekey/action"
      ],
      "AssignableScopes": ["/subscriptions/'$AZURE_SUBSCRIPTION_ID'"]
      }'
   ```
   _(Optional) If you are using a different Subscription for backups and cluster resources, make sure to specify both subscriptions
   inside `AssignableScopes`._

### Option 1: Create service principal

#### Create service principal

1. Obtain your Azure Account Tenant ID:

    ```bash
    AZURE_TENANT_ID=`az account list --query '[?isDefault].tenantId' -o tsv`
    ```

2. Create a service principal.

    If you'll be using Velero to backup multiple clusters with multiple blob containers, it may be desirable to create a unique username per cluster rather than the default `velero`.

    Create service principal and let the CLI generate a password for you. Make sure to capture the password.

    _(Optional) If you are using a different Subscription for backups and cluster resources, make sure to specify both subscriptions
    in the `az` command using `--scopes`._

    ```bash
    AZURE_CLIENT_SECRET=`az ad sp create-for-rbac --name "velero" --role $AZURE_ROLE --query 'password' -o tsv \
      --scopes  /subscriptions/$AZURE_SUBSCRIPTION_ID[ /subscriptions/$AZURE_BACKUP_SUBSCRIPTION_ID]`
    ```

    NOTE: Ensure that value for `--name` does not conflict with other service principals/app registrations.

    After creating the service principal, obtain the client id.

    ```bash
    AZURE_CLIENT_ID=`az ad sp list --display-name "velero" --query '[0].appId' -o tsv`
    ```

3. Now you need to create a file that contains all the relevant environment variables. The command looks like the following:

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

### Option 2: Use AAD Pod Identity

These instructions have been adapted from the [aad-pod-identity documentation][24].

Before proceeding, ensure that you have installed and configured [aad-pod-identity][23] for your cluster.

#### Create identity

1. Create an identity for Velero:

    ```bash
    export IDENTITY_NAME=velero

    az identity create \
        --subscription $AZURE_SUBSCRIPTION_ID \
        --resource-group $AZURE_RESOURCE_GROUP \
        --name $IDENTITY_NAME

    export IDENTITY_CLIENT_ID="$(az identity show -g $AZURE_RESOURCE_GROUP -n $IDENTITY_NAME --subscription $AZURE_SUBSCRIPTION_ID --query clientId -otsv)"
    export IDENTITY_RESOURCE_ID="$(az identity show -g $AZURE_RESOURCE_GROUP -n $IDENTITY_NAME --subscription $AZURE_SUBSCRIPTION_ID --query id -otsv)"    
    ```
    
    If you'll be using Velero to backup multiple clusters with multiple blob containers, it may be desirable to create a unique identity name per cluster rather than the default `velero`.

2. Assign the identity a role:

    ```bash
    export IDENTITY_ASSIGNMENT_ID="$(az role assignment create --role $AZURE_ROLE --assignee $IDENTITY_CLIENT_ID --scope /subscriptions/$AZURE_SUBSCRIPTION_ID --query id -otsv)"
    ```

3. In the cluster, create an `AzureIdentity` and `AzureIdentityBinding`:

    ```bash
    cat <<EOF | kubectl apply -f -
    apiVersion: "aadpodidentity.k8s.io/v1"
    kind: AzureIdentity
    metadata:
      name: $IDENTITY_NAME
    spec:
      type: 0
      resourceID: $IDENTITY_RESOURCE_ID
      clientID: $IDENTITY_CLIENT_ID
    EOF

    cat <<EOF | kubectl apply -f -
    apiVersion: "aadpodidentity.k8s.io/v1"
    kind: AzureIdentityBinding
    metadata:
      name: $IDENTITY_NAME-binding
    spec:
      azureIdentity: $IDENTITY_NAME
      selector: $IDENTITY_NAME
    EOF
    ```

4. Create a file that contains all the relevant environment variables:

    ```bash
    cat << EOF  > ./credentials-velero
    AZURE_SUBSCRIPTION_ID=${AZURE_SUBSCRIPTION_ID}
    AZURE_RESOURCE_GROUP=${AZURE_RESOURCE_GROUP}
    AZURE_CLOUD_NAME=AzurePublicCloud
    EOF
    ```

    > available `AZURE_CLOUD_NAME` values: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`, `AzureGermanCloud`


### Option 3: Use storage account access key

_Note: this option is **not valid** if you are planning to take Azure snapshots of your managed disks with Velero._

1. Obtain your Azure Storage account access key:

    ```bash
    AZURE_STORAGE_ACCOUNT_ACCESS_KEY=`az storage account keys list --account-name $AZURE_STORAGE_ACCOUNT_ID --query "[?keyName == 'key1'].value" -o tsv`
    ```

1. Now you need to create a file that contains all the relevant environment variables. The command looks like the following:

    ```bash
    cat << EOF  > ./credentials-velero
    AZURE_STORAGE_ACCOUNT_ACCESS_KEY=${AZURE_STORAGE_ACCOUNT_ACCESS_KEY}
    AZURE_CLOUD_NAME=AzurePublicCloud
    EOF
    ```

    > available `AZURE_CLOUD_NAME` values: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`, `AzureGermanCloud`

## Install and start Velero

[Download][6] Velero

Install Velero, including all prerequisites, into the cluster and start the deployment. This will create a namespace called `velero`, and place a deployment named `velero` in it.

**If using service principal or AAD Pod Identity:**

```bash
velero install \
    --provider azure \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.6.0 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_ID[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
    --snapshot-location-config apiTimeout=<YOUR_TIMEOUT>[,resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID]
```

If you're using **AAD Pod Identity**, you now need to add the `aadpodidbinding=$IDENTITY_NAME` label to the Velero pod(s), preferably through the Deployment's pod template.  

**If using storage account access key and no Azure snapshots:**

```bash
velero install \
    --provider azure \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.6.0 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_ID,storageAccountKeyEnvVar=AZURE_STORAGE_ACCOUNT_ACCESS_KEY[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
    --use-volume-snapshots=false
```

Additionally, you can specify `--use-node-agent` to enable node agent support, and `--wait` to wait for the deployment to be ready.

### Optional installation steps
1. Specify [additional configurable parameters][7] for the `--backup-location-config` flag.
1. Specify [additional configurable parameters][8] for the `--snapshot-location-config` flag.
1. [Customize the Velero installation][9] further to meet your needs.
1. Velero does not officially [support for Windows containers][10]. If your cluster has both Windows and Linux agent pool, add a node selector to the `velero` deployment to run Velero only on the Linux nodes. This can be done using the below command.
    ```bash
    kubectl patch deploy velero --namespace velero --type merge --patch '{ \"spec\": { \"template\": { \"spec\": { \"nodeSelector\": { \"beta.kubernetes.io/os\": \"linux\"} } } } }'
    ```


For more complex installation needs, use either the Helm chart, or add `--dry-run -o yaml` options for generating the YAML representation for the installation.

## Create an additional Backup Storage Location

If you are using Velero v1.6.0 or later, you can create additional Azure [Backup Storage Locations][13] that use their own credentials.
These can also be created alongside Backup Storage Locations that use other providers.

### Limitations
It is not possible to use different credentials for additional Backup Storage Locations if you are pod based authentication such as [AAD Pod Identity][13].

### Prerequisites

* Velero 1.6.0 or later
* Azure plugin must be installed, either at install time, or by running `velero plugin add velero/velero-plugin-for-microsoft-azure:plugin-version`, replace the `plugin-version` with the corresponding value

### Configure the blob container and credentials

To configure a new Backup Storage Location with its own credentials, it is necessary to follow the steps above to [create the storage account and blob container to use][1], and generate the credentials file to interact with that blob container.
You can either [create a service principal][15] or [use a storage account access key][16] to create the credentials file.
Once you have created the credentials file, create a [Kubernetes Secret][17] in the Velero namespace that contains these credentials:

```bash
kubectl create secret generic -n velero bsl-credentials --from-file=azure=</path/to/credentialsfile>
```

This will create a secret named `bsl-credentials` with a single key (`azure`) which contains the contents of your credentials file.
The name and key of this secret will be given to Velero when creating the Backup Storage Location, so it knows which secret data to use.

### Create Backup Storage Location

Once the bucket and credentials have been configured, these can be used to create the new Backup Storage Location.

If you are using a service principal, create the Backup Storage Location as follows:

```bash
velero backup-location create <bsl-name> \
  --provider azure \
  --bucket $BLOB_CONTAINER \
  --config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_ID[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
  --credential=bsl-credentials=azure
```

Otherwise, use the following command if you are using a storage account access key:

```bash
velero backup-location create <bsl-name> \
  --provider azure \
  --bucket $BLOB_CONTAINER \
  --config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_ID,storageAccountKeyEnvVar=AZURE_STORAGE_ACCOUNT_ACCESS_KEY[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
  --credential=bsl-credentials=azure
```

The Backup Storage Location is ready to use when it has the phase `Available`.
You can check this with the following command:

```bash
velero backup-location get
```

To use this new Backup Storage Location when performing a backup, use the flag `--storage-location <bsl-name>` when running `velero backup create`.

## Extra security measures

To improve security within Azure, it's good practice [to disable public traffic to your Azure Storage Account][26]. If your AKS cluster is in the same Azure Region as your storage account, access to your Azure Storage Account should be easily enabled by a [Virtual Network endpoint][27] on your VNet.

[1]: #Create-Azure-storage-account-and-blob-container
[2]: #Set-permissions-for-Velero
[3]: #Install-and-start-Velero
[4]: #Get-resource-group-containing-your-VMs-and-disks
[6]: https://velero.io/docs/install-overview/
[7]: backupstoragelocation.md
[8]: volumesnapshotlocation.md
[9]: https://velero.io/docs/customize-installation/
[10]:https://velero.io/docs/v1.4/basic-install/#velero-on-windows
[11]: https://velero.io/docs/faq/
[12]: #create-an-additional-backup-storage-location
[13]: https://velero.io/docs/latest/api-types/backupstoragelocation/
[14]: #option-2-use-aad-pod-identity
[15]: #option-1-create-service-principal
[16]: #option-3-use-storage-account-access-key
[17]: https://kubernetes.io/docs/concepts/configuration/secret/
[20]: https://docs.microsoft.com/en-us/azure/active-directory/develop/active-directory-application-objects
[21]: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli
[22]: https://docs.microsoft.com/en-us/azure/architecture/best-practices/naming-conventions#storage
[23]: https://github.com/Azure/aad-pod-identity
[24]: https://github.com/Azure/aad-pod-identity#demo
[25]: https://azure.microsoft.com/en-us/services/kubernetes-service/
[26]: https://docs.microsoft.com/en-us/azure/storage/common/storage-network-security
[27]: https://docs.microsoft.com/en-us/azure/virtual-network/virtual-network-service-endpoints-overview
[101]: https://github.com/vmware-tanzu/velero-plugin-for-microsoft-azure/workflows/Main%20CI/badge.svg
[102]: https://github.com/vmware-tanzu/velero-plugin-for-microsoft-azure/actions?query=workflow%3A"Main+CI"
[103]: https://github.com/vmware-tanzu/velero/issues/new/choose 
