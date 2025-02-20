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

/*
This file provides a compatability layer for velero util package for azure storage
ref. https://github.com/vmware-tanzu/velero/blob/main/pkg/util/azure/storage.go
*/
package util

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	azure "github.com/vmware-tanzu/velero/pkg/util/azure"
)

const (
	ApiVersion string = "apiVersion"
	serviceNameBlob cloud.ServiceName = "blob"
)

func init() {
	cloud.AzureChina.Services[serviceNameBlob] = cloud.ServiceConfiguration{
		Endpoint: "blob.core.chinacloudapi.cn",
	}
	cloud.AzureGovernment.Services[serviceNameBlob] = cloud.ServiceConfiguration{
		Endpoint: "blob.core.usgovcloudapi.net",
	}
	cloud.AzurePublic.Services[serviceNameBlob] = cloud.ServiceConfiguration{
		Endpoint: "blob.core.windows.net",
	}
}

// getStorageAccountURI returns the storage account URI by the following order:
// 1. Return the storage account URI directly fi it is not specified in the BSL config
// 2. Try and call Azure API to get the storage account URI if possible(Background: https://github.com/vmware-tanzu/velero/issues/6163)
// 3. Fall back to return the defualt URI
// ref. https://github.com/vmware-tanzu/velero/blob/main/pkg/util/azure/storage.go#L167
func getStorageAccountURI(log logrus.FieldLogger, bslCfg map[string]string, creds map[string]string) (string, error) {
	uri := bslCfg[azure.BSLConfigStorageAccountURI]
	if uri != "" {
		log.Infof("the storage account URI %q is specified in the BSL, use it directly", uri)
		return uri, nil
	}

	storageAccount := bslCfg[azure.BSLConfigStorageAccount]

	cloudCfg, err := getCloudConfiguration(bslCfg, creds)
	if err != nil {
		return "", err
	}

	uri = fmt.Sprintf("https://%s.%s", storageAccount, cloudCfg.Services["blob"].Endpoint)

	// the storage account access key cannot be used to get the storage account properties,
	// so fallback to the default URI
	if name := bslCfg[azure.BSLConfigStorageAccountAccessKeyName]; name != "" && creds[name] != "" {
		log.Infof("auth with the storage account access key, cannot retrieve the storage account properties, fallback to use the default URI %q", uri)
		return uri, nil
	}

	client, err := newStorageAccountManagemenClient(bslCfg, creds)
	if err != nil {
		log.Infof("failed to create the storage account management client: %v, fallback to use the default URI %q", err, uri)
		return uri, nil
	}

	resourceGroup := azure.GetFromLocationConfigOrCredential(bslCfg, creds, azure.BSLConfigResourceGroup, azure.CredentialKeyResourceGroup)
	// we cannot get the storage account properties without the resource group, so fallback to the default URI
	if resourceGroup == "" {
		log.Infof("resource group isn't set which is required to retrieve the storage account properties, fallback to use the default URI %q", uri)
		return uri, nil
	}

	properties, err := client.GetProperties(context.Background(), resourceGroup, storageAccount, nil)
	// get error, fallback to the default URI
	if err != nil {
		log.Infof("failed to retrieve the storage account properties: %v, fallback to use the default URI %q", err, uri)
		return uri, nil
	}

	uri = *properties.Account.Properties.PrimaryEndpoints.Blob
	log.Infof("use the storage account URI retrieved from the storage account properties %q", uri)

	return uri, nil
}

// GetClientOptions returns the client options based on the BSL/VSL config and credentials
func GetClientOptions(locationCfg, creds map[string]string) (policy.ClientOptions, error) {
	options := policy.ClientOptions{}

	cloudCfg, err := getCloudConfiguration(locationCfg, creds)
	if err != nil {
		return options, err
	}
	options.Cloud = cloudCfg

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

	if locationCfg[ApiVersion] != "" {
		options.APIVersion = locationCfg[ApiVersion]
	}

	return options, nil
}

// NewStorageClient creates a blob storage client(data plane) with the provided config which contains BSL config and the credential file name.
// The returned azblob.SharedKeyCredential is needed for Azure plugin to generate the SAS URL when auth with storage
// account access key
// ref. https://github.com/vmware-tanzu/velero/blob/main/pkg/util/azure/storage.go#L63
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

	// resolve the storage account uri
	uri, err := getStorageAccountURI(log, config, creds)

	// TODO : Support APIVersion and other policy options
	clientOptions, err := GetClientOptions(bslCfg, creds)
	if err != nil {
		return nil, nil, err
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

// new a management client for the storage account
func newStorageAccountManagemenClient(bslCfg map[string]string, creds map[string]string) (*armstorage.AccountsClient, error) {
	clientOptions, err := GetClientOptions(bslCfg, creds)
	if err != nil {
		return nil, err
	}

	cred, err := azure.NewCredential(creds, clientOptions)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create Azure AD credential")
	}

	subID := azure.GetFromLocationConfigOrCredential(bslCfg, creds, azure.BSLConfigSubscriptionID, azure.CredentialKeySubscriptionID)
	if subID == "" {
		return nil, errors.New("subscription ID is required in BSL or credential to create the storage account client")
	}

	client, err := armstorage.NewAccountsClient(subID, cred, &arm.ClientOptions{
		ClientOptions: clientOptions,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the storage account client")
	}

	return client, nil
}