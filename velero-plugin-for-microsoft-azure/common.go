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
	"fmt"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

const (
	subscriptionIDEnvVar = "AZURE_SUBSCRIPTION_ID"
	cloudNameEnvVar      = "AZURE_CLOUD_NAME"

	resourceGroupConfigKey   = "resourceGroup"
	credentialsFileConfigKey = "credentialsFile"
	subscriptionIDConfigKey  = "subscriptionId"

	Storage cloud.ServiceName = "BlobStorage"
)

// credentialsFileFromEnv retrieves the Azure credentials file from the environment.
func credentialsFileFromEnv() string {
	return os.Getenv("AZURE_CREDENTIALS_FILE")
}

// selectCredentialsFile selects the Azure credentials file to use, retrieving it
// from the given config or falling back to retrieving it from the environment.
func selectCredentialsFile(config map[string]string) (string, error) {
	if credentialsFile, ok := config[credentialsFileConfigKey]; ok {
		// Check that the provided credentialsFile exists on disk
		if _, err := os.Stat(credentialsFile); err != nil {
			if os.IsNotExist(err) {
				return "", errors.Wrapf(err, "provided credentialsFile does not exist")
			}
			return "", errors.Wrapf(err, "could not get credentialsFile info")
		}
		return credentialsFile, nil
	}

	return credentialsFileFromEnv(), nil
}

// loadCredentialsIntoEnv loads the variables in the given credentials
// file into the current environment.
func loadCredentialsIntoEnv(credentialsFile string) error {
	if credentialsFile == "" {
		return nil
	}

	if err := godotenv.Overload(credentialsFile); err != nil {
		return errors.Wrapf(err, "error loading environment from credentials file (%s)", credentialsFile)
	}

	return nil
}

// map cloud names from go-autorest to cloud config from azure-sdk
// add the storage endpoint
func cloudFromName(cloudName string) (cloud.Configuration, error) {
	fmt.Println(cloudName)
	switch strings.ToUpper(cloudName) {
	case "AZURECHINACLOUD":
		config := cloud.AzureChina
		config.Services[Storage] = cloud.ServiceConfiguration{
			Endpoint: "core.chinacloudapi.cn",
		}
		return config, nil
	case "", "AZURECLOUD", "AZUREPUBLICCLOUD":
		config := cloud.AzurePublic
		config.Services[Storage] = cloud.ServiceConfiguration{
			Endpoint: "core.windows.net",
		}
		return config, nil
	case "AZUREUSGOVERNMENT", "AZUREUSGOVERNMENTCLOUD":
		config := cloud.AzureGovernment
		config.Services[Storage] = cloud.ServiceConfiguration{
			Endpoint: "core.usgovcloudapi.net",
		}
		return config, nil
	default:
		return cloud.Configuration{}, fmt.Errorf("there is no cloud matching the name %q", cloudName)
	}
}

func getRequiredValues(getValue func(string) string, keys ...string) (map[string]string, error) {
	missing := []string{}
	results := map[string]string{}

	for _, key := range keys {
		if val := getValue(key); val == "" {
			missing = append(missing, key)
		} else {
			results[key] = val
		}
	}

	if len(missing) > 0 {
		return nil, errors.Errorf("the following keys do not have values: %s", strings.Join(missing, ", "))
	}

	return results, nil
}

func mapLookup(data map[string]string) func(string) string {
	return func(key string) string {
		return data[key]
	}
}
