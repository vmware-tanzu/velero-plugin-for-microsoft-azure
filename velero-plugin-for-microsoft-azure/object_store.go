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
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	storagemgmt "github.com/Azure/azure-sdk-for-go/services/storage/mgmt/2019-06-01/storage"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

const (
	storageAccountConfigKey          = "storageAccount"
	storageAccountKeyEnvVarConfigKey = "storageAccountKeyEnvVar"
	subscriptionIDConfigKey          = "subscriptionId"
	blockSizeConfigKey               = "blockSizeInBytes"

	// blocks must be less than/equal to 100MB in size
	// ref. https://docs.microsoft.com/en-us/rest/api/storageservices/put-block#uri-parameters
	defaultBlockSize = 100 * 1024 * 1024
)

type containerGetter interface {
	getContainer(bucket string) (container, error)
}

type azureContainerGetter struct {
	blobService *storage.BlobStorageClient
}

func (cg *azureContainerGetter) getContainer(bucket string) (container, error) {
	container := cg.blobService.GetContainerReference(bucket)
	if container == nil {
		return nil, errors.Errorf("unable to get container reference for bucket %v", bucket)
	}

	return &azureContainer{
		container: container,
	}, nil
}

type container interface {
	ListBlobs(params storage.ListBlobsParameters) (storage.BlobListResponse, error)
}

type azureContainer struct {
	container *storage.Container
}

func (c *azureContainer) ListBlobs(params storage.ListBlobsParameters) (storage.BlobListResponse, error) {
	return c.container.ListBlobs(params)
}

type blobGetter interface {
	getBlob(bucket, key string) (blob, error)
}

type azureBlobGetter struct {
	blobService *storage.BlobStorageClient
}

func (bg *azureBlobGetter) getBlob(bucket, key string) (blob, error) {
	container := bg.blobService.GetContainerReference(bucket)
	if container == nil {
		return nil, errors.Errorf("unable to get container reference for bucket %v", bucket)
	}

	blob := container.GetBlobReference(key)
	if blob == nil {
		return nil, errors.Errorf("unable to get blob reference for key %v", key)
	}

	return &azureBlob{
		blob: blob,
	}, nil
}

type blob interface {
	PutBlock(blockID string, chunk []byte, options *storage.PutBlockOptions) error
	PutBlockList(blocks []storage.Block, options *storage.PutBlockListOptions) error
	Exists() (bool, error)
	Get(options *storage.GetBlobOptions) (io.ReadCloser, error)
	Delete(options *storage.DeleteBlobOptions) error
	GetSASURI(options *storage.BlobSASOptions) (string, error)
}

type azureBlob struct {
	blob *storage.Blob
}

func (b *azureBlob) PutBlock(blockID string, chunk []byte, options *storage.PutBlockOptions) error {
	return b.blob.PutBlock(blockID, chunk, options)
}
func (b *azureBlob) PutBlockList(blocks []storage.Block, options *storage.PutBlockListOptions) error {
	return b.blob.PutBlockList(blocks, options)
}

func (b *azureBlob) Exists() (bool, error) {
	return b.blob.Exists()
}

func (b *azureBlob) Get(options *storage.GetBlobOptions) (io.ReadCloser, error) {
	return b.blob.Get(options)
}

func (b *azureBlob) Delete(options *storage.DeleteBlobOptions) error {
	return b.blob.Delete(options)
}

func (b *azureBlob) GetSASURI(options *storage.BlobSASOptions) (string, error) {
	return b.blob.GetSASURI(*options)
}

type ObjectStore struct {
	log             logrus.FieldLogger
	containerGetter containerGetter
	blobGetter      blobGetter
	blockSize       int
}

func newObjectStore(logger logrus.FieldLogger) *ObjectStore {
	return &ObjectStore{log: logger}
}

// getSubscriptionID gets the subscription ID from the 'config' map if it contains
// it, else from the AZURE_SUBSCRIPTION_ID environment variable.
func getSubscriptionID(config map[string]string) string {
	if subscriptionID := config[subscriptionIDConfigKey]; subscriptionID != "" {
		return subscriptionID
	}

	return os.Getenv(subscriptionIDEnvVar)
}

func getStorageAccountKey(config map[string]string) (string, *azure.Environment, error) {
	credentialsFile, err := selectCredentialsFile(config)
	if err != nil {
		return "", nil, err
	}

	if err := loadCredentialsIntoEnv(credentialsFile); err != nil {
		return "", nil, err
	}

	// get Azure cloud from AZURE_CLOUD_NAME, if it exists. If the env var does not
	// exist, parseAzureEnvironment will return azure.PublicCloud.
	env, err := parseAzureEnvironment(os.Getenv(cloudNameEnvVar))
	if err != nil {
		return "", nil, errors.Wrap(err, "unable to parse azure cloud name environment variable")
	}

	// get storage account key from env var whose name is in config[storageAccountKeyEnvVarConfigKey].
	// If the config does not exist, continue obtaining the storage key using API
	if secretKeyEnvVar := config[storageAccountKeyEnvVarConfigKey]; secretKeyEnvVar != "" {
		storageKey := os.Getenv(secretKeyEnvVar)
		if storageKey == "" {
			return "", env, errors.Errorf("no storage account key found in env var %s", secretKeyEnvVar)
		}

		return storageKey, env, nil
	}

	// get subscription ID from object store config or AZURE_SUBSCRIPTION_ID environment variable
	subscriptionID := getSubscriptionID(config)
	if subscriptionID == "" {
		return "", nil, errors.New("azure subscription ID not found in object store's config or in environment variable")
	}

	// we need config["resourceGroup"], config["storageAccount"]
	if _, err := getRequiredValues(mapLookup(config), resourceGroupConfigKey, storageAccountConfigKey); err != nil {
		return "", env, errors.Wrap(err, "unable to get all required config values")
	}

	// get authorizer from environment in the following order:
	// 1. client credentials (AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_CLIENT_SECRET)
	// 2. client certificate (AZURE_CERTIFICATE_PATH, AZURE_CERTIFICATE_PASSWORD)
	// 3. username and password (AZURE_USERNAME, AZURE_PASSWORD)
	// 4. MSI (managed service identity)
	authorizer, err := auth.NewAuthorizerFromEnvironment()
	if err != nil {
		return "", nil, errors.Wrap(err, "error getting authorizer from environment")
	}

	// get storageAccountsClient
	storageAccountsClient := storagemgmt.NewAccountsClientWithBaseURI(env.ResourceManagerEndpoint, subscriptionID)
	storageAccountsClient.Authorizer = authorizer

	// get storage key
	res, err := storageAccountsClient.ListKeys(context.TODO(), config[resourceGroupConfigKey], config[storageAccountConfigKey], storagemgmt.Kerb)
	if err != nil {
		return "", env, errors.WithStack(err)
	}
	if res.Keys == nil || len(*res.Keys) == 0 {
		return "", env, errors.New("No storage keys found")
	}

	var storageKey string
	for _, key := range *res.Keys {
		// uppercase both strings for comparison because the ListKeys call returns e.g. "FULL" but
		// the storagemgmt.Full constant in the SDK is defined as "Full".
		if strings.ToUpper(string(key.Permissions)) == strings.ToUpper(string(storagemgmt.Full)) {
			storageKey = *key.Value
			break
		}
	}

	if storageKey == "" {
		return "", env, errors.New("No storage key with Full permissions found")
	}

	return storageKey, env, nil
}

func mapLookup(data map[string]string) func(string) string {
	return func(key string) string {
		return data[key]
	}
}

func (o *ObjectStore) Init(config map[string]string) error {
	if err := veleroplugin.ValidateObjectStoreConfigKeys(config,
		resourceGroupConfigKey,
		storageAccountConfigKey,
		subscriptionIDConfigKey,
		blockSizeConfigKey,
		storageAccountKeyEnvVarConfigKey,
		credentialsFileConfigKey,
	); err != nil {
		return err
	}

	storageAccountKey, env, err := getStorageAccountKey(config)
	if err != nil {
		return err
	}

	// 6. get storageClient and blobClient
	if _, err := getRequiredValues(mapLookup(config), storageAccountConfigKey); err != nil {
		return errors.Wrap(err, "unable to get all required config values")
	}

	storageClient, err := storage.NewBasicClientOnSovereignCloud(config[storageAccountConfigKey], storageAccountKey, *env)
	if err != nil {
		return errors.Wrap(err, "error getting storage client")
	}

	blobClient := storageClient.GetBlobService()
	o.containerGetter = &azureContainerGetter{
		blobService: &blobClient,
	}
	o.blobGetter = &azureBlobGetter{
		blobService: &blobClient,
	}

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

	if blockSize <= 0 || blockSize > defaultBlockSize {
		log.WithError(err).Warnf("Value provided for config.blockSizeInBytes (%d) is outside the allowed range of 1 to %d, using default block size of %d", blockSize, defaultBlockSize, defaultBlockSize)
		return defaultBlockSize
	}

	return blockSize
}

func (o *ObjectStore) PutObject(bucket, key string, body io.Reader) error {
	blob, err := o.blobGetter.getBlob(bucket, key)
	if err != nil {
		return err
	}

	// Azure requires a blob/object to be chunked if it's larger than 256MB. Since we
	// don't know ahead of time if the body is over this limit or not, and it would
	// require reading the entire object into memory to determine the size, we use the
	// chunking approach for all objects.

	var (
		block    = make([]byte, o.blockSize)
		blockIDs []storage.Block
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

			blockIDs = append(blockIDs, storage.Block{
				ID:     blockID,
				Status: storage.BlockStatusLatest,
			})
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
	blob, err := o.blobGetter.getBlob(bucket, key)
	if err != nil {
		return false, err
	}

	exists, err := blob.Exists()
	if err != nil {
		return false, errors.WithStack(err)
	}

	return exists, nil
}

func (o *ObjectStore) GetObject(bucket, key string) (io.ReadCloser, error) {
	blob, err := o.blobGetter.getBlob(bucket, key)
	if err != nil {
		return nil, err
	}

	res, err := blob.Get(nil)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return res, nil
}

func (o *ObjectStore) ListCommonPrefixes(bucket, prefix, delimiter string) ([]string, error) {
	container, err := o.containerGetter.getContainer(bucket)
	if err != nil {
		return nil, err
	}

	params := storage.ListBlobsParameters{
		Prefix:    prefix,
		Delimiter: delimiter,
	}

	var prefixes []string
	for {
		res, err := container.ListBlobs(params)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		prefixes = append(prefixes, res.BlobPrefixes...)
		if res.NextMarker == "" {
			break
		}
		params.Marker = res.NextMarker
	}

	return prefixes, nil
}

func (o *ObjectStore) ListObjects(bucket, prefix string) ([]string, error) {
	container, err := o.containerGetter.getContainer(bucket)
	if err != nil {
		return nil, err
	}

	params := storage.ListBlobsParameters{
		Prefix: prefix,
	}

	var objects []string
	for {
		res, err := container.ListBlobs(params)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		for _, blob := range res.Blobs {
			objects = append(objects, blob.Name)
		}
		if res.NextMarker == "" {
			break
		}
		params.Marker = res.NextMarker
	}

	return objects, nil
}

func (o *ObjectStore) DeleteObject(bucket string, key string) error {
	blob, err := o.blobGetter.getBlob(bucket, key)
	if err != nil {
		return err
	}

	return errors.WithStack(blob.Delete(nil))
}

func (o *ObjectStore) CreateSignedURL(bucket, key string, ttl time.Duration) (string, error) {
	blob, err := o.blobGetter.getBlob(bucket, key)
	if err != nil {
		return "", err
	}

	opts := storage.BlobSASOptions{
		SASOptions: storage.SASOptions{
			Expiry: time.Now().Add(ttl),
		},
		BlobServiceSASPermissions: storage.BlobServiceSASPermissions{
			Read: true,
		},
	}

	return blob.GetSASURI(&opts)
}
