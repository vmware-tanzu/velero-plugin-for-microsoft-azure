package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"

	azure "github.com/vmware-tanzu/velero/pkg/util/azure"
)

func getCloudConfiguration(locationCfg, creds map[string]string) (cloud.Configuration, error) {
	env := &azclient.Environment{}
	cloudName := creds[azure.CredentialKeyCloudName]
	activeDirectoryAuthorityURI := locationCfg[azure.BSLConfigActiveDirectoryAuthorityURI]

	config := azclient.AzureCloudConfigFromName(cloudName)

	if locationCfg["resourceManagerEndpoint"] != "" {
		err := azclient.OverrideAzureCloudConfigAndEnvConfigFromMetadataService(locationCfg["resourceManagerEndpoint"], cloudName, config, env)
		if err != nil {
			return *config, err
		}
	}

	err := azclient.OverrideAzureCloudConfigFromEnv(cloudName, config, env)
	if err != nil {
		return *config, err
	}

	if activeDirectoryAuthorityURI != "" {
		config.ActiveDirectoryAuthorityHost = activeDirectoryAuthorityURI
	}
	return *config, err
}

func getStorageAccountURI(log logrus.FieldLogger, bslCfg map[string]string, creds map[string]string, cloud cloud.Configuration) (string, error) {
	uri := bslCfg[azure.BSLConfigStorageAccountURI]
	if uri != "" {
		log.Infof("the storage account URI %q is specified in the BSL, use it directly", uri)
		return uri, nil
	}

	storageAccount := bslCfg[azure.BSLConfigStorageAccount]

	// XXX : should we use this endpoint or the environment?
	uri = fmt.Sprintf("https://%s.%s", storageAccount, cloud.Services["blob"].Endpoint)

	// TODO: Support the other lookup methods for this

	return uri, nil
}

func getClientOptions(locationCfg, creds map[string]string, cloud cloud.Configuration) (policy.ClientOptions, error) {
	options := policy.ClientOptions{
		Cloud: cloud,
	}

	if locationCfg["caCert"] != "" {
		certPool, _ := x509.SystemCertPool()
		if certPool == nil {
			certPool = x509.NewCertPool()
		}
		var caCert []byte
		var err error
		// As this function is used in both repository and plugin, the caCert isn't encoded
		// when passing to the plugin while is encoded when works with repository, use one
		// config item to distinguish these two cases
		if locationCfg["caCertEncoded"] != "" {
			caCert, err = base64.StdEncoding.DecodeString(locationCfg["caCert"])
			if err != nil {
				return options, err
			}
		} else {
			caCert = []byte(locationCfg["caCert"])
		}

		certPool.AppendCertsFromPEM(caCert)

		// https://github.com/Azure/azure-sdk-for-go/blob/sdk/azcore/v1.6.1/sdk/azcore/runtime/transport_default_http_client.go#L19
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    certPool,
			},
		}
		options.Transport = &http.Client{
			Transport: transport,
		}
	}

	if locationCfg["apiVersion"] != "" {
		options.APIVersion = locationCfg["apiVersion"]
	}

	return options, nil
}

func NewStorageClient(log logrus.FieldLogger, config map[string]string) (*azblob.Client, *azblob.SharedKeyCredential, error) {
	// rename to bslCfg for easy understanding
	bslCfg := config

	// storage account is required
	storageAccount := bslCfg[azure.BSLConfigStorageAccount]
	if storageAccount == "" {
		return nil, nil, errors.Errorf("%s is required in BSL", azure.BSLConfigStorageAccount)
	}

	// read the credentials provided by users
	creds, err := azure.LoadCredentials(config)
	if err != nil {
		return nil, nil, err
	}
	// exchange the storage account access key if needed
	creds, err = azure.GetStorageAccountCredentials(bslCfg, creds)
	if err != nil {
		return nil, nil, err
	}

	// resolve the cloud using the kubernetes cloud provider for azure
	cloud, err := getCloudConfiguration(bslCfg, creds)
	if err != nil {
		return nil, nil, err
	}

	// resolve the storage account uri
	uri, err := getStorageAccountURI(log, config, creds, cloud)

	// TODO : Support APIVersion and other policy options
	clientOptions := policy.ClientOptions{
		Cloud: cloud,
	}
	
	blobClientOptions := &azblob.ClientOptions{
		ClientOptions: clientOptions,
	}

	// auth with storage account access key
	accessKey := creds[azure.CredentialKeyStorageAccountAccessKey]
	if accessKey != "" {
		log.Info("auth with the storage account access key")
		cred, err := azblob.NewSharedKeyCredential(storageAccount, accessKey)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to create storage account access key credential")
		}
		client, err := azblob.NewClientWithSharedKeyCredential(uri, cred, blobClientOptions)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to create blob client with the storage account access key")
		}
		return client, cred, nil
	}

	// auth with Azure AD
	log.Info("auth with Azure AD")
	cred, err := azure.NewCredential(creds, clientOptions)
	if err != nil {
		return nil, nil, err
	}
	client, err := azblob.NewClient(uri, cred, blobClientOptions)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create blob client with the Azure AD credential")
	}
	return client, nil, nil
}