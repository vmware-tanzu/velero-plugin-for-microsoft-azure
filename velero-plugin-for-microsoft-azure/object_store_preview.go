package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

const (
	blob_url_suffix = "https://%s.blob.core.windows.net"
)

type ObjectStorePreview struct {
	log        logrus.FieldLogger
	credential *azblob.SharedKeyCredential
	pipeline   *pipeline.Pipeline
	service    *azblob.ServiceURL
	cpk        *azblob.ClientProvidedKeyOptions
}

func (o *ObjectStorePreview) Init(config map[string]string) error {
	if err := veleroplugin.ValidateObjectStoreConfigKeys(config,
		resourceGroupConfigKey,
		storageAccountConfigKey,
		subscriptionIDConfigKey,
		storageAccountKeyEnvVarConfigKey,
	); err != nil {
		return err
	}

	// DEBUG
	key := "MDEyMzQ1NjcwMTIzNDU2NzAxMjM0NTY3MDEyMzQ1Njc="
	hash := "3QFFFpRA5+XANHqwwbT4yXDmrT/2JaLt/FKHjzhOdoE="
	scope := ""
	cpk := azblob.NewClientProvidedKeyOptions(&key, &hash, &scope)

	storageAccountKey, _, err := getStorageAccountKey(config)
	if err != nil {
		return err
	}

	cred, err := azblob.NewSharedKeyCredential(config[storageAccountConfigKey], storageAccountKey)
	if err != nil {
		return err
	}

	u, _ := url.Parse(fmt.Sprintf(blob_url_suffix, config[storageAccountConfigKey]))
	if err != nil {
		return err
	}

	pipeline := azblob.NewPipeline(cred, azblob.PipelineOptions{})
	service := azblob.NewServiceURL(*u, pipeline)

	o.credential = cred
	o.pipeline = &pipeline
	o.service = &service
	o.cpk = &cpk

	return nil
}

func (o *ObjectStorePreview) PutObject(bucket, key string, body io.Reader) error {
	container := o.service.NewContainerURL(bucket)
	blobURL := container.NewBlockBlobURL(key)
	_, err := azblob.UploadStreamToBlockBlob(context.Background(), body, blobURL, azblob.UploadStreamToBlockBlobOptions{ClientProvidedKeyOptions: *o.cpk})

	if err != nil {
		return err
	}
	return nil
}

func (o *ObjectStorePreview) ObjectExists(bucket, key string) (bool, error) {
	ctx := context.Background()
	container := o.service.NewContainerURL(bucket)
	blob := container.NewBlobURL(key)
	_, err := blob.GetProperties(ctx, azblob.BlobAccessConditions{}, *o.cpk)

	if err == nil {
		return true, err
	}

	if storageErr, ok := err.(azblob.StorageError); ok {
		if storageErr.Response().StatusCode == 404 {
			return false, nil
		}
	}

	return false, err
}

func (o *ObjectStorePreview) GetObject(bucket, key string) (io.ReadCloser, error) {
	container := o.service.NewContainerURL(bucket)
	blobURL := container.NewBlockBlobURL(key)
	response, err := blobURL.Download(context.TODO(), 0, azblob.CountToEnd, azblob.BlobAccessConditions{}, false, *o.cpk)
	if err != nil {
		return nil, err
	}

	return response.Body(azblob.RetryReaderOptions{}), nil
}

func (o *ObjectStorePreview) ListCommonPrefixes(bucket, prefix, delimiter string) ([]string, error) {
	return make([]string, 0), nil // This function is not implemented.
}

func (o *ObjectStorePreview) ListObjects(bucket, prefix string) ([]string, error) {
	var objects []string
	container := o.service.NewContainerURL(bucket)
	marker := azblob.Marker{}

	for marker.NotDone() {
		listBlob, err := container.ListBlobsFlatSegment(context.Background(), marker, azblob.ListBlobsSegmentOptions{Prefix: prefix})

		if err != nil {
			return nil, err
		}
		marker = listBlob.NextMarker

		for _, blobInfo := range listBlob.Segment.BlobItems {
			objects = append(objects, blobInfo.Name)
		}
	}
	return objects, nil
}

func (o *ObjectStorePreview) DeleteObject(bucket string, key string) error {
	container := o.service.NewContainerURL(bucket)
	blobURL := container.NewBlockBlobURL(key)
	_, err := blobURL.Delete(context.Background(), azblob.DeleteSnapshotsOptionNone, azblob.BlobAccessConditions{})
	if err != nil {
		return err
	}
	return nil
}

func (o *ObjectStorePreview) CreateSignedURL(bucket, key string, ttl time.Duration) (string, error) {
	sasQueryParams, err := azblob.BlobSASSignatureValues{
		Protocol:      azblob.SASProtocolHTTPS,
		ExpiryTime:    time.Now().UTC().Add(ttl),
		ContainerName: bucket,
		BlobName:      key,
		Permissions:   azblob.BlobSASPermissions{Add: false, Read: true, Write: false}.String()}.NewSASQueryParameters(o.credential)
	if err != nil {
		log.Fatal(err)
	}

	qp := sasQueryParams.Encode()
	SasUri := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s?%s",
		o.credential.AccountName(), bucket, key, qp)

	return SasUri, errors.New("Not Implemented")
}
