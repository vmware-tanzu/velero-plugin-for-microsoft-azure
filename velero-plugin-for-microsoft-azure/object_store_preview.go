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
	pipeline  *pipeline.Pipeline
	context   *context.Context
	service   *azblob.ServiceURL
	container *azblob.ContainerURL
}

func (o *ObjectStorePreview) Init(config map[string]string) error {
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

	storageAccountKey, _, err := getStorageAccountKey(config)
	if err != nil {
		return err
	}

	cred, err := azblob.NewSharedKeyCredential(storageAccountConfigKey, storageAccountKey)
	if err != nil {
		return err
	}

	u, _ := url.Parse(fmt.Sprintf(blob_url_suffix, storageAccountConfigKey))
	if err != nil {
		return err
	}

	pipeline := azblob.NewPipeline(cred, azblob.PipelineOptions{})
	context := context.Background()
	service := azblob.NewServiceURL(*u, pipeline)
	container := service.NewContainerURL(config["containerName"])

	o.pipeline = &pipeline
	o.context = &context
	o.service = &service
	o.container = &container

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
	return make([]string, 0), nil
}

func (o *ObjectStorePreview) DeleteObject(bucket string, key string) error {
	return nil
}

func (o *ObjectStorePreview) CreateSignedURL(bucket, key string, ttl time.Duration) (string, error) {
	return "", nil
}
