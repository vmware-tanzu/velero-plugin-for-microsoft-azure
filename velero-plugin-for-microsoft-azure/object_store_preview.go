package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-storage-blob-go/azblob"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

const (
	blob_url_suffix = "https://%s.blob.core.windows.net"
)

type ObjectStorePreview struct {
	pipeline *pipeline.Pipeline
	context  *context.Context
	service  *azblob.ServiceURL
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

	o.pipeline = &pipeline
	o.service = &service

	return nil
}

func (o *ObjectStorePreview) PutObject(bucket, key string, body io.Reader) error {
	return nil
}

func (o *ObjectStorePreview) ObjectExists(bucket, key string) (bool, error) {
	return false, nil
}

func (o *ObjectStorePreview) GetObject(bucket, key string) (io.ReadCloser, error) {
	return &io.PipeReader{}, nil
}

func (o *ObjectStorePreview) ListCommonPrefixes(bucket, prefix, delimiter string) ([]string, error) {
	return make([]string, 0), nil
}

func (o *ObjectStorePreview) ListObjects(bucket, prefix string) ([]string, error) {
	var objects []string
	ctx := context.Background()

	container := o.service.NewContainerURL(bucket)

	marker := azblob.Marker{}
	for marker.NotDone() {
		listBlob, err := container.ListBlobsFlatSegment(ctx, marker, azblob.ListBlobsSegmentOptions{})

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
	return nil
}

func (o *ObjectStorePreview) CreateSignedURL(bucket, key string, ttl time.Duration) (string, error) {
	return "", nil
}
