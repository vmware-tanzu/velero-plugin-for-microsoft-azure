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
This file provides a compatability layer for velero util package azure
ref. https://github.com/vmware-tanzu/velero/blob/main/pkg/util/azure/util.go

The goal is to change the cloud resolution mechanism from an internal discovery to
the same mechanisms provided by sigs.k8s.io/cloud-provider-azure
ref. https://github.com/kubernetes-sigs/cloud-provider-azure/blob/master/pkg/azclient/cloud.go
*/
package util

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	azure "github.com/vmware-tanzu/velero/pkg/util/azure"
	azclient "sigs.k8s.io/cloud-provider-azure/pkg/azclient"
)

const (
	// Credential key which enables cloud lookup using the metadata host.
	resourceManagerEndpoint string = "AZURE_METADATA_HOST"
)

// getCloudConfiguration based on the BSL/VSL config and credentials
// ref. https://github.com/vmware-tanzu/velero/blob/main/pkg/util/azure/util.go#L134
func getCloudConfiguration(locationCfg, creds map[string]string) (cloud.Configuration, error) {
	name := creds[azure.CredentialKeyCloudName]
	activeDirectoryAuthorityURI := locationCfg[azure.BSLConfigActiveDirectoryAuthorityURI]

	var cfg cloud.Configuration
	switch strings.ToUpper(name) {
	case "", "AZURECLOUD", "AZUREPUBLICCLOUD":
		cfg = cloud.AzurePublic
	case "AZURECHINACLOUD":
		cfg = cloud.AzureChina
	case "AZUREUSGOVERNMENT", "AZUREUSGOVERNMENTCLOUD":
		cfg = cloud.AzureGovernment
	default:
		env := &azclient.Environment{}
		cfg = cloud.Configuration{
			Services: map[cloud.ServiceName]cloud.ServiceConfiguration{},
		}
		if creds[resourceManagerEndpoint] != "" {
			err := azclient.OverrideAzureCloudConfigAndEnvConfigFromMetadataService(creds[resourceManagerEndpoint], name, &cfg, env)
			if err != nil {
				return cloud.Configuration{}, err
			}
		}
		err := azclient.OverrideAzureCloudConfigFromEnv(name, &cfg, env)
		if err != nil {
			return cloud.Configuration{}, err
		}
		if env.StorageEndpointSuffix == "" {
			return cloud.Configuration{}, fmt.Errorf("unknown cloud: %s", name)
		}

		// Compatability with what the velero server expects
		cfg = cloud.Configuration{
			ActiveDirectoryAuthorityHost: cfg.ActiveDirectoryAuthorityHost,
			Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
				serviceNameBlob: cloud.ServiceConfiguration{
					Endpoint: env.StorageEndpointSuffix,
				},
			},
		}
	}
	if activeDirectoryAuthorityURI != "" {
		cfg.ActiveDirectoryAuthorityHost = activeDirectoryAuthorityURI
	}
	return cfg, nil
}