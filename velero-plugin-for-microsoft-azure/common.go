/*
Copyright 2018, 2020 the Velero contributors.

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

	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

const (
	subscriptionIDEnvVar = "AZURE_SUBSCRIPTION_ID"
	cloudNameEnvVar      = "AZURE_CLOUD_NAME"

	resourceGroupConfigKey = "resourceGroup"
	credentialsFileKey     = "credentialsFile"
)

func loadEnvFromCredentialsFile(config map[string]string) error {
	// Prioritize the credentials file path in config, if it exists
	credentialsFile, ok := config[credentialsFileKey]
	if ok {
		// Check that the provided credentialsFile exists on disk
		if _, err := os.Stat(credentialsFile); err != nil {
			if os.IsNotExist(err) {
				return errors.Wrapf(err, "provided credentialsFile does not exist")
			}
			return errors.Wrapf(err, "could not get credentialsFile info")
		}
	} else {
		// If a credentials file does not exist in the config, fall back to
		// any credentials file specified in the environment.
		credentialsFile = os.Getenv("AZURE_CREDENTIALS_FILE")
	}

	if credentialsFile == "" {
		return nil
	}

	if err := godotenv.Overload(credentialsFile); err != nil {
		return errors.Wrapf(err, "error loading environment from credentials file (%s)", credentialsFile)
	}

	return nil
}

func loadEnv() error {
	return loadEnvFromCredentialsFile(map[string]string{})
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
