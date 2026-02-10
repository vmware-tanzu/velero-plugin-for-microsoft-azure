
[![Build Status][101]][102]

# Velero plugins for Microsoft Azure

## Overview

This repository contains these plugins to support running Velero on Microsoft Azure:

- An object store plugin for persisting and retrieving backups on Azure Blob Storage. Content of backup is kubernetes resources and metadata files for CSI objects, progress of async operations. It is also used to store the result data of backups and restores include log files, warning/error files, etc.

- A volume snapshotter plugin for creating snapshots from volumes (during a backup) and volumes from snapshots (during a restore) on Azure Managed Disks.
  - Since v1.4.0 the snapshotter plugin can handle the volumes provisioned by CSI driver `disk.csi.azure.com`.
  - Since v1.5.0 the snapshotter plugin can handle the zone-redundant storage(ZRS) managed disks which can be used to support backup/restore across different available zones.

## Compatibility

Below is a listing of plugin versions and respective Velero versions that are compatible.

| Plugin Version | Velero Version |
|----------------|----------------|
| v1.13.x        | v1.17.x        |
| v1.12.x        | v1.16.x        |
| v1.11.x        | v1.15.x        |
| v1.10.x        | v1.14.x        |
| v1.9.x         | v1.13.x        |



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
    --min-tls-version TLS1_2 \
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

There are several ways Velero can authenticate to Azure: (1) by using a Velero-specific [service principal][20] with secret-based authentication; (2) by using a Velero-specific [service principal][20] with certificate-based authentication; (3) by using [Azure AD Workload Identity][23]; or (4) by using a storage account access key.

If you plan to use Velero to take Azure snapshots of your persistent volume managed disks, you **must** use the service principal or Azure AD Workload Identity method.

If you don't plan to take Azure disk snapshots, any method is valid.

### Specify Role
_**Note**: This is only required for (1) by using a Velero-specific service principal with secret-based authentication, (2) by using a Velero-specific service principal with certificate-based authentication and (3) by using Azure AD Workload Identity._

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

   > Note: With useAAD flag you will need to provide extra permissions `Storage Blob Data Contributor` covered in point 3 of section: [Create service principal](#create-service-principal)

   Here are the minimum required permissions needed by Velero to perform backups, restores, and deletions:
   - Storage Account
      > Back Compatability and Restic
      - Microsoft.Storage/storageAccounts/read
      - Microsoft.Storage/storageAccounts/listkeys/action
      - Microsoft.Storage/storageAccounts/regeneratekey/action
      > AAD Based Auth
      - Microsoft.Storage/storageAccounts/read
      - Microsoft.Storage/storageAccounts/blobServices/containers/delete
      - Microsoft.Storage/storageAccounts/blobServices/containers/read
      - Microsoft.Storage/storageAccounts/blobServices/containers/write
      - Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action
      > Data Actions for AAD auth
      - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/delete
      - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read
      - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write
      - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/move/action
      - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/add/action
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
          "Microsoft.Storage/storageAccounts/regeneratekey/action",
          "Microsoft.Storage/storageAccounts/read",
          "Microsoft.Storage/storageAccounts/blobServices/containers/delete",
          "Microsoft.Storage/storageAccounts/blobServices/containers/read",
          "Microsoft.Storage/storageAccounts/blobServices/containers/write",
          "Microsoft.Storage/storageAccounts/blobServices/generateUserDelegationKey/action"
      ],
      "DataActions" :[
        "Microsoft.Storage/storageAccounts/blobServices/containers/blobs/delete",
        "Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read",
        "Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write",
        "Microsoft.Storage/storageAccounts/blobServices/containers/blobs/move/action",
        "Microsoft.Storage/storageAccounts/blobServices/containers/blobs/add/action"
      ],
      "AssignableScopes": ["/subscriptions/'$AZURE_SUBSCRIPTION_ID'"]
      }'
   ```
   _(Optional) If you are using a different Subscription for backups and cluster resources, make sure to specify both subscriptions
   inside `AssignableScopes`._

### Option 1: Create service principal with secret-based authentication

1. Obtain your Azure Account Tenant ID:

    ```bash
    AZURE_TENANT_ID=`az account list --query '[?isDefault].tenantId' -o tsv`
    ```

2. Create a service principal with secet-based authentication.

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
3. (Optional)Assign additional permissions to the service principal (For useAAD=true with built-in role)

    If you use the custom role which has the blob data permissions, skip this step.

    If you chose the AAD route, this is an additional permissions required for the service principal to be able to access the storage account.
    ```bash
    az role assignment create --assignee $AZURE_CLIENT_ID --role "Storage Blob Data Contributor" --scope /subscriptions/$AZURE_SUBSCRIPTION_ID
    ```

    Refer: [useAAD parameter in BackupStorageLocation.md](./backupstoragelocation.md#backup-storage-location)

4. Now you need to create a file that contains all the relevant environment variables. The command looks like the following:

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

    > Available values for `AZURE_CLOUD_NAME`: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`

### Option 2: Create service principal with certificate-based authentication

1. Obtain your Azure Account Tenant ID:

    ```bash
    AZURE_TENANT_ID=`az account list --query '[?isDefault].tenantId' -o tsv`
    ```

2. Create a service principal with certificate-based authentication.

    If you'll be using Velero to backup multiple clusters with multiple blob containers, it may be desirable to create a unique username per cluster rather than the default `velero`.

    Create service principal and let the CLI creates a self-signed certificate for you. Make sure to capture the certificate.

    _(Optional) If you are using a different Subscription for backups and cluster resources, make sure to specify both subscriptions
    in the `az` command using `--scopes`._

    ```bash
    AZURE_CLIENT_CERTIFICATE_PATH=`az ad sp create-for-rbac --name "velero" --role $AZURE_ROLE --query 'fileWithCertAndPrivateKey' -o tsv \
      --scopes  /subscriptions/$AZURE_SUBSCRIPTION_ID[ /subscriptions/$AZURE_BACKUP_SUBSCRIPTION_ID] --create-cert`
    ```

    NOTE: Ensure that value for `--name` does not conflict with other service principals/app registrations.

    After creating the service principal, obtain the client id.

    ```bash
    AZURE_CLIENT_ID=`az ad sp list --display-name "velero" --query '[0].appId' -o tsv`
    ```
3. (Optional)Assign additional permissions to the service principal (For useAAD=true with built-in role)

    If you use the custom role which has the blob data permissions, skip this step.

    If you chose the AAD route, this is an additional permissions required for the service principal to be able to access the storage account.
    ```bash
    az role assignment create --assignee $AZURE_CLIENT_ID --role "Storage Blob Data Contributor" --scope /subscriptions/$AZURE_SUBSCRIPTION_ID
    ```

    Refer: [useAAD parameter in BackupStorageLocation.md](./backupstoragelocation.md#backup-storage-location)

6. Now you need to create a file that contains all the relevant environment variables. The command looks like the following:

    ```bash
    cat << EOF  > ./credentials-velero
    AZURE_SUBSCRIPTION_ID=${AZURE_SUBSCRIPTION_ID}
    AZURE_TENANT_ID=${AZURE_TENANT_ID}
    AZURE_CLIENT_ID=${AZURE_CLIENT_ID}
    AZURE_CLIENT_CERTIFICATE=$(awk 'BEGIN {printf "\""} {sub(/\r/, ""); printf "%s\\n",$0;} END {printf "\""}' $AZURE_CLIENT_CERTIFICATE_PATH)
    AZURE_RESOURCE_GROUP=${AZURE_RESOURCE_GROUP}
    AZURE_CLOUD_NAME=AzurePublicCloud
    EOF
    ```

    > Available values for `AZURE_CLOUD_NAME`: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`

### Option 3: Use Azure AD Workload Identity

These instructions have been adapted from the [Azure AD Workload Identity Quick Start][24] documentation.

Before proceeding, ensure that you have installed [workload identity mutating admission webhook][28] and [enabled the OIDC Issuer][29] for your cluster.

1. Create an identity for Velero:

    ```bash
    IDENTITY_NAME=velero

    az identity create \
        --subscription $AZURE_SUBSCRIPTION_ID \
        --resource-group $AZURE_RESOURCE_GROUP \
        --name $IDENTITY_NAME

    IDENTITY_CLIENT_ID="$(az identity show -g $AZURE_RESOURCE_GROUP -n $IDENTITY_NAME --subscription $AZURE_SUBSCRIPTION_ID --query clientId -otsv)"
    ```

    If you'll be using Velero to backup multiple clusters with multiple blob containers, it may be desirable to create a unique identity name per cluster rather than the default `velero`.

2. Assign the identity roles:

    ```bash
    az role assignment create --role $AZURE_ROLE --assignee $IDENTITY_CLIENT_ID --scope /subscriptions/$AZURE_SUBSCRIPTION_ID
    ```

    (Optional)Assign additional permissions to the service principal (For useAAD=true with built-in role)

    If you use the custom role which has the blob data permissions, skip this step.

    If you chose the AAD route, this is an additional permissions required for the identity to be able to access the storage account.
    ```bash
    az role assignment create --assignee $IDENTITY_CLIENT_ID --role "Storage Blob Data Contributor" --scope /subscriptions/$AZURE_SUBSCRIPTION_ID
    ```

    Refer: [useAAD parameter in BackupStorageLocation.md](./backupstoragelocation.md#backup-storage-location)

3. Create a service account for Velero

    ```bash
    # create namespace
    kubectl create namespace velero

    # create service account
    cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      annotations:
        azure.workload.identity/client-id: $IDENTITY_CLIENT_ID
      name: velero
      namespace: velero
    EOF

    # create clusterrolebinding
    cat <<EOF | kubectl apply -f -
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: velero
    subjects:
    - kind: ServiceAccount
      name: velero
      namespace: velero
    roleRef:
      kind: ClusterRole
      name: cluster-admin
      apiGroup: rbac.authorization.k8s.io
    EOF
    ```

4. Get the cluster OIDC issuer URL

    ```bash
    CLUSTER_RESOURCE_GROUP=<NAME_OF_CLUSTER_RESOURCE_GROUP>
    ```
    **WARNING**: If you're using [AKS][25], `CLUSTER_RESOURCE_GROUP` must be set to the name of the resource group where the cluster is created, not the auto-generated resource group that is created when you provision your cluster in Azure.

    ```bash
    CLUSTER_NAME=your_cluster_name

    SERVICE_ACCOUNT_ISSUER=$(az aks show --resource-group $CLUSTER_RESOURCE_GROUP --name $CLUSTER_NAME --query "oidcIssuerProfile.issuerUrl" -o tsv)
    ```

5. Establish federated identity credential between the identity and the service account issuer & subject
    ```bash
    az identity federated-credential create \
      --name "kubernetes-federated-credential" \
      --identity-name "${IDENTITY_NAME}" \
      --resource-group "${AZURE_RESOURCE_GROUP}" \
      --issuer "${SERVICE_ACCOUNT_ISSUER}" \
      --subject "system:serviceaccount:velero:velero"
    ```

6. Create a file that contains all the relevant environment variables:

    ```bash
    cat << EOF  > ./credentials-velero
    AZURE_SUBSCRIPTION_ID=${AZURE_SUBSCRIPTION_ID}
    AZURE_RESOURCE_GROUP=${AZURE_RESOURCE_GROUP}
    AZURE_CLOUD_NAME=AzurePublicCloud
    EOF
    ```

    > Available values for `AZURE_CLOUD_NAME`: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`


### Option 4: Use storage account access key

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

    > Available values for `AZURE_CLOUD_NAME`: `AzurePublicCloud`, `AzureUSGovernmentCloud`, `AzureChinaCloud`

## Install and start Velero

[Download][6] Velero

Install Velero, including all prerequisites, into the cluster and start the deployment. This will create a namespace called `velero`, and place a deployment named `velero` in it.

### If using service principal:

```bash
velero install \
    --provider azure \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.13.0 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config useAAD="true",resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
    --snapshot-location-config apiTimeout=<YOUR_TIMEOUT>[,resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID]
```

### If using Azure AD Workload Identity:

```bash
velero install \
    --provider azure \
    --service-account-name velero \
    --pod-labels azure.workload.identity/use=true \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.13.0 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config useAAD="true",resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
    --snapshot-location-config apiTimeout=<YOUR_TIMEOUT>[,resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID]
```

In plugin v1.8.0+, users can chose to use AAD route for velero to access storage account when using service principal or Azure AD Workload Identity. Earlier this was done using ListKeys on the storage account which is not a recommended practice.

**Limitation:** The velero identity needs Reader permission alongside the "storage blob data contributor" role on the storage account. This is because the identity needs to be able to read the storage account properties to fetch the storage account's blob endpoint (azure storage accounts are no longer expected to follow the format of blob.core.windows.net with introduction of DNS zone storage accounts.). To circumvent this issue follow the steps below:

**For users facing Storage Account Throttling issues**
You can chose to provide the Storage Account's blob endpoint directly to Velero. This will help Velero to bypass the need to fetch the storage account properties and hence the need for Reader permission on the storage account. This can be done by providing the blob endpoint in the backup-location-config as shown below:

```bash
velero install \
    --provider azure \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.13.0 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config storageAccountURI="https://xxxxxx.blob.core.windows.net",useAAD="true",resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
    --snapshot-location-config apiTimeout=<YOUR_TIMEOUT>[,resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID]
```

Note:
- If you have provided the storageAccountUri, providing the resourceGroup and storageAccount fields are optional.

**Migrating from ListKeys to AAD route:**
If you already had a velero setup using azure plugin < 1.8.0 it must be using the ListKeys approach which is not recommended. To migrate to the AAD route follow the steps below:

You need to assign your velero identity the following permissions:

```bash
az role assignment create --role "Storage Blob Data Contributor" --assignee $IDENTITY_CLIENT_ID --scope /subscriptions/$AZURE_SUBSCRIPTION_ID/resourceGroups/$AZURE_BACKUP_RESOURCE_GROUP/providers/Microsoft.Storage/storageAccounts/$AZURE_STORAGE_ACCOUNT_ID
```

After that update your velero BackupStorageLocation with the useAAD flag as shown below:

```bash
velero backup-location set default --provider azure --bucket $BLOB_CONTAINER --config useAAD="true",resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID]
```

Limitation: Listing storage account access key is still needed for Restic to work as expected on Azure. The useAAD route won't accrue to it and users using Restic should not remove the ListKeys permission from the velero identity.

### If using storage account access key and no Azure snapshots:

```bash
velero install \
    --provider azure \
    --plugins velero/velero-plugin-for-microsoft-azure:v1.13.0 \
    --bucket $BLOB_CONTAINER \
    --secret-file ./credentials-velero \
    --backup-location-config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME,storageAccountKeyEnvVar=AZURE_STORAGE_ACCOUNT_ACCESS_KEY[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
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
It is not possible to use different credentials for additional Backup Storage Locations if you are pod based authentication such as [Azure AD Workload Identity][23].

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
  --config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
  --credential=bsl-credentials=azure
```

Otherwise, use the following command if you are using a storage account access key:

```bash
velero backup-location create <bsl-name> \
  --provider azure \
  --bucket $BLOB_CONTAINER \
  --config resourceGroup=$AZURE_BACKUP_RESOURCE_GROUP,storageAccount=$AZURE_STORAGE_ACCOUNT_NAME,storageAccountKeyEnvVar=AZURE_STORAGE_ACCOUNT_ACCESS_KEY[,subscriptionId=$AZURE_BACKUP_SUBSCRIPTION_ID] \
  --credential=bsl-credentials=azure
```

If you would like to customize the Storage Location and or the AAD URI associated with the backup location add the following to the `--config` argument:
```bash
--config storageAccountURI='https://my-sa.blob.core.windows.net',activeDirectoryAuthorityURI='https://login.microsoftonline.us/'
```

The Backup Storage Location is ready to use when it has the phase `Available`.
You can check this with the following command:

```bash
velero backup-location get
```

To use this new Backup Storage Location when performing a backup, use the flag `--storage-location <bsl-name>` when running `velero backup create`.

## Extra security measures

To improve security within Azure, it's good practice [to disable public traffic to your Azure Storage Account][26]. If your AKS cluster is in the same Azure Region as your storage account, access to your Azure Storage Account should be easily enabled by a [Virtual Network endpoint][27] on your VNet.

## Tips
We recommend taking incremental snapshots of Azure Disks since they are more cost efficient and come with the following benefits (Read more: [Azure Docs][30]):

> Incremental snapshots are point-in-time backups for managed disks that, when taken, consist only of the changes since the last snapshot. The first incremental snapshot is a full copy of the disk. The subsequent incremental snapshots occupy only delta changes to disks since the last snapshot. When you restore a disk from an incremental snapshot, the system reconstructs the full disk that represents the point in time backup of the disk when the incremental snapshot was taken.

> If ZRS is available in the selected region, an incremental snapshot will use ZRS automatically. If ZRS isn't available in the region, then the snapshot will default to locally-redundant storage (LRS)

**To enable Incremental snapshots, set `incremental` to `true` as part of `--snapshot-location-config`. Refer [additional configurable parameters][8] for the `--snapshot-location-config` flag.**

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
[15]: #option-1-create-service-principal
[16]: #option-3-use-storage-account-access-key
[17]: https://kubernetes.io/docs/concepts/configuration/secret/
[20]: https://docs.microsoft.com/en-us/azure/active-directory/develop/active-directory-application-objects
[21]: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli
[22]: https://docs.microsoft.com/en-us/azure/architecture/best-practices/naming-conventions#storage
[23]: https://azure.github.io/azure-workload-identity/docs/introduction.html
[24]: https://azure.github.io/azure-workload-identity/docs/quick-start.html
[25]: https://azure.microsoft.com/en-us/services/kubernetes-service/
[26]: https://docs.microsoft.com/en-us/azure/storage/common/storage-network-security
[27]: https://docs.microsoft.com/en-us/azure/virtual-network/virtual-network-service-endpoints-overview
[28]: https://azure.github.io/azure-workload-identity/docs/installation/mutating-admission-webhook.html
[29]: https://learn.microsoft.com/en-us/azure/aks/use-oidc-issuer#create-an-aks-cluster-with-oidc-issuer
[30]: https://learn.microsoft.com/en-us/azure/virtual-machines/disks-incremental-snapshots
[101]: https://github.com/vmware-tanzu/velero-plugin-for-microsoft-azure/workflows/Main%20CI/badge.svg
[102]: https://github.com/vmware-tanzu/velero-plugin-for-microsoft-azure/actions?query=workflow%3A"Main+CI"
[103]: https://github.com/vmware-tanzu/velero/issues/new/choose
