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
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	azcontainer "github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/vmware-tanzu/velero-plugin-for-microsoft-azure/velero-plugin-for-microsoft-azure/util"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
	"github.com/vmware-tanzu/velero/pkg/util/azure"
)

const (
	blockSizeConfigKey = "blockSizeInBytes"
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
	// we need to keep the credential here to create the sas url
	sharedKeyCredential *azblob.SharedKeyCredential
}

func newObjectStore(logger logrus.FieldLogger) *ObjectStore {
	return &ObjectStore{log: logger}
}

// Init sets up the ObjectStore using the shared key or default azure credentials
func (o *ObjectStore) Init(config map[string]string) error {
	if err := veleroplugin.ValidateObjectStoreConfigKeys(config,
		azure.BSLConfigResourceGroup,
		azure.BSLConfigStorageAccount,
		azure.BSLConfigSubscriptionID,
		blockSizeConfigKey,
		azure.BSLConfigActiveDirectoryAuthorityURI,
		azure.BSLConfigStorageAccountURI,
		azure.BSLConfigUseAAD,
		azure.BSLConfigStorageAccountAccessKeyName,
		credentialsFileConfigKey,
		util.ApiVersion,
	); err != nil {
		return err
	}

	client, cred, err := util.NewStorageClient(o.log, config)
	if err != nil {
		return err
	}
	o.sharedKeyCredential = cred

	o.containerGetter = &azureContainerGetter{
		serviceClient: client.ServiceClient(),
	}
	o.blobGetter = &azureBlobGetter{
		serviceClient: client.ServiceClient(),
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
