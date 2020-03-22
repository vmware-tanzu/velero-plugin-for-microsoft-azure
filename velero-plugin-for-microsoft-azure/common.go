/*
Copyright 2018 the Velero contributors.

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
	"os"
	"strings"

	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

const (
	tenantIDEnvVar       = "AZURE_TENANT_ID"
	subscriptionIDEnvVar = "AZURE_SUBSCRIPTION_ID"
	clientIDEnvVar       = "AZURE_CLIENT_ID"
	clientSecretEnvVar   = "AZURE_CLIENT_SECRET"
	cloudNameEnvVar      = "AZURE_CLOUD_NAME"

	resourceGroupConfigKey = "resourceGroup"
)

func loadEnv() error {
	envFile := os.Getenv("AZURE_CREDENTIALS_FILE")
	if envFile == "" {
		return nil
	}

	if err := godotenv.Overload(envFile); err != nil {
		return errors.Wrapf(err, "error loading environment from AZURE_CREDENTIALS_FILE (%s)", envFile)
	}

	return nil
}

// ParseAzureEnvironment returns an azure.Environment for the given cloud
// name, or azure.PublicCloud if cloudName is empty.
func parseAzureEnvironment(cloudName string) (*azure.Environment, error) {
	if cloudName == "" {
		return &azure.PublicCloud, nil
	}

	env, err := azure.EnvironmentFromName(cloudName)
	return &env, errors.WithStack(err)
}

func newServicePrincipalToken(tenantID, clientID, clientSecret string, env *azure.Environment) (*adal.ServicePrincipalToken, error) {
	oauthConfig, err := adal.NewOAuthConfig(env.ActiveDirectoryEndpoint, tenantID)
	if err != nil {
		return nil, errors.Wrap(err, "error getting OAuthConfig")
	}

	return adal.NewServicePrincipalToken(*oauthConfig, clientID, clientSecret, env.ResourceManagerEndpoint)
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
