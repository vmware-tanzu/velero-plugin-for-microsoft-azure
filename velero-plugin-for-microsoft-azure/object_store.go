/*
Copyright the Velero contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	azcontainer "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

const (
	storageAccountConfigKey              = "storageAccount"
	storageAccountKeyEnvVarConfigKey     = "storageAccountKeyEnvVar"
	blockSizeConfigKey                   = "blockSizeInBytes"
	storageAccountURIConfigKey           = "storageAccountURI"
	useAADConfigKey                      = "useAAD"
	activeDirectoryAuthorityURIConfigKey = "activeDirectoryAuthorityURI"

	// blocks must be less than/equal to 100MB in size
	// ref. https://docs.microsoft.com/en-us/rest/api/storageservices/put-block#uri-parameters
	maxBlockSize     = 100 * 1024 * 1024
	defaultBlockSize = 1 * 1024 * 1024
)

type containerGetter interface {
	getContainer(bucket string) container
}

type azureContainerGetter struct {
	serviceClient *service.Client
}

func (cg *azureContainerGetter) getContainer(bucket string) container {
	containerClient := cg.serviceClient.NewContainerClient(bucket)

	return &azureContainer{
		containerClient: containerClient,
	}
}

type container interface {
	ListBlobs(params *azcontainer.ListBlobsFlatOptions) *runtime.Pager[azcontainer.ListBlobsFlatResponse]
	ListBlobsHierarchy(delimiter string, listOptions *azcontainer.ListBlobsHierarchyOptions) *runtime.Pager[azcontainer.ListBlobsHierarchyResponse]
}

type azureContainer struct {
	containerClient *azcontainer.Client
}

func (c *azureContainer) ListBlobs(params *azcontainer.ListBlobsFlatOptions) *runtime.Pager[azcontainer.ListBlobsFlatResponse] {
	return c.containerClient.NewListBlobsFlatPager(params)
}

func (c *azureContainer) ListBlobsHierarchy(delimiter string, listOptions *azcontainer.ListBlobsHierarchyOptions) *runtime.Pager[azcontainer.ListBlobsHierarchyResponse] {
	return c.containerClient.NewListBlobsHierarchyPager(delimiter, listOptions)
}

type blobGetter interface {
	getBlob(bucket, key string) blob
}

type azureBlobGetter struct {
	serviceClient *service.Client
}

func (bg *azureBlobGetter) getBlob(bucket, key string) blob {
	containerClient := bg.serviceClient.NewContainerClient(bucket)
	blobClient := containerClient.NewBlockBlobClient(key)
	return &azureBlob{
		container:     bucket,
		blob:          key,
		blobClient:    blobClient,
		serviceClient: bg.serviceClient,
	}
}

type blob interface {
	PutBlock(blockID string, chunk []byte, options *blockblob.StageBlockOptions) error
	PutBlockList(blocks []string, options *blockblob.CommitBlockListOptions) error
	Exists() (bool, error)
	Get(options *azblob.DownloadStreamOptions) (io.ReadCloser, error)
	Delete(options *azblob.DeleteBlobOptions) error
	GetSASURI(duration time.Duration, sharedKeyCredential *azblob.SharedKeyCredential) (string, error)
}

type azureBlob struct {
	container     string
	blob          string
	blobClient    *blockblob.Client
	serviceClient *service.Client
}

type nopCloser struct {
	io.ReadSeeker
}

func (n nopCloser) Close() error {
	return nil
}

// NopCloser returns a ReadSeekCloser with a no-op close method wrapping the provided io.ReadSeeker.
func NopCloser(rs io.ReadSeeker) io.ReadSeekCloser {
	return nopCloser{rs}
}

func (b *azureBlob) PutBlock(blockID string, chunk []byte, options *blockblob.StageBlockOptions) error {
	_, err := b.blobClient.StageBlock(context.TODO(), blockID, NopCloser(bytes.NewReader(chunk)), options)
	return err
}

func (b *azureBlob) PutBlockList(blocks []string, options *blockblob.CommitBlockListOptions) error {
	_, err := b.blobClient.CommitBlockList(context.TODO(), blocks, options)
	return err
}

func (b *azureBlob) Exists() (bool, error) {
	_, err := b.blobClient.GetProperties(context.TODO(), nil)
	if err == nil {
		return true, nil
	}
	if bloberror.HasCode(err, bloberror.ContainerNotFound, bloberror.BlobNotFound) {
		return false, nil
	}
	return false, err
}

func (b *azureBlob) Get(options *azblob.DownloadStreamOptions) (io.ReadCloser, error) {
	res, err := b.blobClient.BlobClient().DownloadStream(context.TODO(), options)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return res.Body, nil
}

func (b *azureBlob) Delete(options *azblob.DeleteBlobOptions) error {
	_, err := b.blobClient.Delete(context.TODO(), options)
	return err
}

// When the sharedKeyCredential is provided service SAS is used else delegation SAS is used
func (b *azureBlob) GetSASURI(ttl time.Duration, sharedKeyCredential *azblob.SharedKeyCredential) (string, error) {
	var queryParam sas.QueryParameters
	var err error
	// because of clock skew it can happen that the token is not yet valid, so make it valid in the past
	startTime := time.Now().Add(-10 * time.Minute).UTC()
	expiryTime := time.Now().Add(ttl).UTC()
	blobSignatureValues := sas.BlobSignatureValues{
		ContainerName: b.container,
		BlobName:      b.blob,
		Protocol:      sas.ProtocolHTTPS,
		StartTime:     startTime,
		ExpiryTime:    expiryTime,
		Permissions:   to.Ptr(sas.BlobPermissions{Read: true}).String(),
	}

	if sharedKeyCredential == nil {
		var udc *service.UserDelegationCredential
		info := service.KeyInfo{
			Start:  to.Ptr(startTime.Format(sas.TimeFormat)),
			Expiry: to.Ptr(expiryTime.Format(sas.TimeFormat)),
		}
		udc, err = b.serviceClient.GetUserDelegationCredential(context.TODO(), info, nil)

		if err != nil {
			return "", err
		}
		queryParam, err = blobSignatureValues.SignWithUserDelegation(udc)
	} else {
		queryParam, err = blobSignatureValues.SignWithSharedKey(sharedKeyCredential)
	}
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s?%s", b.blobClient.URL(), queryParam.Encode())
	return url, nil
}

type ObjectStore struct {
	log logrus.FieldLogger

	containerGetter containerGetter
	blobGetter      blobGetter
	blockSize       int
	storageAccount  string
	// we need to keep the credential here to create the sas url
	sharedKeyCredential *azblob.SharedKeyCredential
}

func newObjectStore(logger logrus.FieldLogger) *ObjectStore {
	return &ObjectStore{log: logger}
}

// get storage account key from env var whose name is in config[storageAccountKeyEnvVarConfigKey].
func getStorageAccountKey(config map[string]string) (string, error) {
	secretKeyEnvVar := config[storageAccountKeyEnvVarConfigKey]
	if secretKeyEnvVar != "" {
		return os.Getenv(secretKeyEnvVar), nil
	}

	return "", nil
}

func populateEnvVarsFromCredentialsFile(config map[string]string) error {
	credentialsFile, err := selectCredentialsFile(config)
	if err != nil {
		return err
	}
	if err := loadCredentialsIntoEnv(credentialsFile); err != nil {
		return err
	}
	return nil
}

// getServiceClient creates a client via SharedKeyCredential or DefaultAzureCredential
func getServiceClient(storageAccount, subscription, resouceGroup string, storageAccountURI string, useAAD string, sharedKeyCredential *azblob.SharedKeyCredential, cloud cloud.Configuration, log logrus.FieldLogger) (*service.Client, *azblob.SharedKeyCredential, error) {
	localSharedKeyCredential := sharedKeyCredential
	clientOptions := policy.ClientOptions{Cloud: cloud}
	var serviceClient *service.Client

	credential, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{ClientOptions: policy.ClientOptions{Cloud: cloud}})
	if err != nil {
		return nil, nil, errors.Wrap(err, "error getting credentials from environment")
	}
	serviceURL := getBlobServicUrl(credential, clientOptions, subscription, resouceGroup, storageAccount, storageAccountURI, cloud.Services[Storage].Endpoint, log)
	if sharedKeyCredential == nil {
		// doc: https://github.com/Azure/azure-sdk-for-go/tree/main/sdk/azidentity
		if strings.ToLower(useAAD) == "true" {
			serviceClient, err = service.NewClient(serviceURL, credential, &service.ClientOptions{ClientOptions: clientOptions})
			return serviceClient, nil, errors.Wrapf(err, "error creating service client with AAD credentials for storage account %s", serviceURL)
		}

		// else fetch the storage account key
		storageAccountKey, err := fetchStorageAccountKey(credential, clientOptions, subscription, resouceGroup, storageAccount)
		if err != nil {
			return nil, nil, err
		}
		localSharedKeyCredential, err = azblob.NewSharedKeyCredential(storageAccount, storageAccountKey)
		if err != nil {
			return nil, nil, err
		}

	}
	serviceClient, err = service.NewClientWithSharedKeyCredential(serviceURL, localSharedKeyCredential, &service.ClientOptions{ClientOptions: clientOptions})
	return serviceClient, localSharedKeyCredential, err
}

func getBlobServicUrl(credential *azidentity.DefaultAzureCredential, clientOptions policy.ClientOptions, subscription, resouceGroup, storageAccount, storageAccountURI, defaultStorageEndpointSuffix string, log logrus.FieldLogger) string {
	// We pass in Uri, since we might need to validate this in future against GetProperties.
	if storageAccountURI != "" {
		return storageAccountURI
	}
	accountClient, err := armstorage.NewAccountsClient(subscription, credential, &arm.ClientOptions{ClientOptions: clientOptions})
	if err != nil {
		log.Debugf("error creating storage account client: %v,  falling back to default SA URL forming mechanism", err)
	} else {
		properties, err := accountClient.GetProperties(context.TODO(), resouceGroup, storageAccount, nil)
		if err != nil {
			log.Debugf("error getting storage account properties: %v, please provide Microsoft.Storage/storageAccounts/read, falling back to default SA URL forming mechanism", err)
		} else {
			return *properties.Account.Properties.PrimaryEndpoints.Blob
		}
	}

	// fallback to default endpoint
	return fmt.Sprintf("https://%s.blob.%s", storageAccount, defaultStorageEndpointSuffix)
}

// fetch the storage account key using the default credential.
// this is deprecated and will be removed in a future release.
func fetchStorageAccountKey(credential *azidentity.DefaultAzureCredential, clientOptions policy.ClientOptions, subscription, resouceGroup, storageAccount string) (string, error) {
	accountClient, err := armstorage.NewAccountsClient(subscription, credential, &arm.ClientOptions{ClientOptions: clientOptions})
	if err != nil {
		return "", err
	}

	res, err := accountClient.ListKeys(context.TODO(), resouceGroup, storageAccount, nil)
	if err != nil {
		return "", err
	}

	for _, key := range res.Keys {
		// ignore case for comparison because the ListKeys call returns e.g. "FULL" but
		// the armstorage.KeyPermissionFull constant in the SDK is defined as "Full".
		if strings.EqualFold(string(*key.Permissions), string(armstorage.KeyPermissionFull)) {
			return *key.Value, nil
		}
	}
	return "", errors.New("No storage key with Full permissions found")
}

// Init sets up the ObjectStore using the shared key or default azure credentials
func (o *ObjectStore) Init(config map[string]string) error {
	var serviceClient *service.Client
	if err := veleroplugin.ValidateObjectStoreConfigKeys(config,
		resourceGroupConfigKey,
		storageAccountConfigKey,
		subscriptionIDConfigKey,
		blockSizeConfigKey,
		storageAccountURIConfigKey,
		useAADConfigKey,
		activeDirectoryAuthorityURIConfigKey,
		storageAccountKeyEnvVarConfigKey,
		credentialsFileConfigKey,
	); err != nil {
		return err
	}

	if err := populateEnvVarsFromCredentialsFile(config); err != nil {
		return err
	}

	if config[storageAccountConfigKey] == "" {
		return errors.Errorf("%s not defined", storageAccountConfigKey)
	}
	o.storageAccount = config[storageAccountConfigKey]

	// get Azure cloud from AZURE_CLOUD_NAME, if it exists. If the env var does not
	// exist, parseAzureEnvironment will return azure.PublicCloud.
	cloudConfig, err := cloudFromName(os.Getenv(cloudNameEnvVar))
	if err != nil {
		return errors.Wrap(err, "unable to parse azure cloud name environment variable")
	}

	// Update active directory authority host if it is set in the configuration
	if config[activeDirectoryAuthorityURIConfigKey] != "" && config[useAADConfigKey] == "true" {
		cloudConfig.ActiveDirectoryAuthorityHost = config[activeDirectoryAuthorityURIConfigKey]
	}

	o.log.Debugf("Getting storage key")
	// optional
	storageAccountKey, err := getStorageAccountKey(config)
	if err != nil {
		o.log.Warnf("Couldn't load storage key: %s", err)
	}
	if storageAccountKey != "" {
		o.sharedKeyCredential, err = azblob.NewSharedKeyCredential(o.storageAccount, storageAccountKey)
		if err != nil {
			return err
		}
	}

	o.log.Debugf("Creating service client")
	subscriptionID := config[subscriptionIDConfigKey]
	if len(subscriptionID) == 0 {
		subscriptionID = os.Getenv(subscriptionIDEnvVar)
	}
	serviceClient, o.sharedKeyCredential, err = getServiceClient(o.storageAccount, subscriptionID, config[resourceGroupConfigKey], config[storageAccountURIConfigKey], config[useAADConfigKey], o.sharedKeyCredential, cloudConfig, o.log)
	if err != nil {
		return err
	}

	o.containerGetter = &azureContainerGetter{
		serviceClient: serviceClient,
	}
	o.blobGetter = &azureBlobGetter{
		serviceClient: serviceClient,
	}

	o.log.Infof("Using storage account key: %t", o.sharedKeyCredential != nil)
	o.log.Debugf("Getting blocksize")
	o.blockSize = getBlockSize(o.log, config)
	return nil
}

func getBlockSize(log logrus.FieldLogger, config map[string]string) int {
	val, ok := config[blockSizeConfigKey]
	if !ok {
		// no alternate block size specified in config, so return with the default
		return defaultBlockSize
	}

	blockSize, err := strconv.Atoi(val)
	if err != nil {
		log.WithError(err).Warnf("Error parsing config.blockSizeInBytes value %v, using default block size of %d", val, defaultBlockSize)
		return defaultBlockSize
	}

	if blockSize <= 0 {
		log.WithError(err).Warnf("Value provided for config.blockSizeInBytes (%d) is < 1, using default block size of %d", blockSize, defaultBlockSize)
		return defaultBlockSize
	}

	if blockSize > maxBlockSize {
		log.WithError(err).Warnf("Value provided for config.blockSizeInBytes (%d) is > the max size %d, using max block size of %d", blockSize, maxBlockSize, maxBlockSize)
		return maxBlockSize
	}

	return blockSize
}

func (o *ObjectStore) PutObject(bucket, key string, body io.Reader) error {
	blob := o.blobGetter.getBlob(bucket, key)
	// Azure requires a blob/object to be chunked if it's larger than 256MB. Since we
	// don't know ahead of time if the body is over this limit or not, and it would
	// require reading the entire object into memory to determine the size, we use the
	// chunking approach for all objects.
	var (
		block    = make([]byte, o.blockSize)
		blockIDs []string
	)

	for {
		n, err := body.Read(block)
		if n > 0 {
			// blockID needs to be the same length for all blocks, so use a fixed width.
			// ref. https://docs.microsoft.com/en-us/rest/api/storageservices/put-block#uri-parameters
			blockID := fmt.Sprintf("%08d", len(blockIDs))

			o.log.Debugf("Putting block (id=%s) of length %d", blockID, n)
			if putErr := blob.PutBlock(blockID, block[0:n], nil); putErr != nil {
				return errors.Wrapf(putErr, "error putting block %s", blockID)
			}

			blockIDs = append(blockIDs, blockID)
		}

		// got an io.EOF: we're done reading chunks from the body
		if err == io.EOF {
			break
		}
		// any other error: bubble it up
		if err != nil {
			return errors.Wrap(err, "error reading block from body")
		}
	}

	o.log.Debugf("Putting block list %v", blockIDs)
	if err := blob.PutBlockList(blockIDs, nil); err != nil {
		return errors.Wrap(err, "error putting block list")
	}

	return nil
}

func (o *ObjectStore) ObjectExists(bucket, key string) (bool, error) {
	blob := o.blobGetter.getBlob(bucket, key)
	exists, err := blob.Exists()
	if err != nil {
		return false, errors.WithStack(err)
	}

	return exists, nil
}

func (o *ObjectStore) GetObject(bucket, key string) (io.ReadCloser, error) {
	blob := o.blobGetter.getBlob(bucket, key)
	return blob.Get(nil)
}

func (o *ObjectStore) ListCommonPrefixes(bucket, prefix, delimiter string) ([]string, error) {
	container := o.containerGetter.getContainer(bucket)
	params := azcontainer.ListBlobsHierarchyOptions{
		Prefix: &prefix,
	}

	var prefixes []string
	pager := container.ListBlobsHierarchy(delimiter, &params)
	for pager.More() {
		page, err := pager.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}

		for _, prefix := range page.ListBlobsHierarchySegmentResponse.Segment.BlobPrefixes {
			prefixes = append(prefixes, *prefix.Name)
		}
	}

	return prefixes, nil
}

func (o *ObjectStore) ListObjects(bucket, prefix string) ([]string, error) {
	container := o.containerGetter.getContainer(bucket)
	params := azcontainer.ListBlobsFlatOptions{
		Prefix: &prefix,
	}

	var objects []string
	pager := container.ListBlobs(&params)
	for pager.More() {
		page, err := pager.NextPage(context.TODO())
		if err != nil {
			return nil, err
		}

		for _, blob := range page.ListBlobsFlatSegmentResponse.Segment.BlobItems {
			objects = append(objects, *blob.Name)
		}
	}
	return objects, nil
}

func (o *ObjectStore) DeleteObject(bucket string, key string) error {
	blob := o.blobGetter.getBlob(bucket, key)
	err := blob.Delete(nil)
	return errors.WithStack(err)
}

func (o *ObjectStore) CreateSignedURL(bucket, key string, ttl time.Duration) (string, error) {
	blob := o.blobGetter.getBlob(bucket, key)
	return blob.GetSASURI(ttl, o.sharedKeyCredential)
}
