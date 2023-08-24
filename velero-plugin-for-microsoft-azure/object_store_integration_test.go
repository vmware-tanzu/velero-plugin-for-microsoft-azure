//go:build integration

/*
Copyright 2018, 2019 the Velero contributors.

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
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"

	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// Storage account and container must be created manually beforehand
// To test with a shared access key the key must be set via env var AZ_STORAGE_KEY
// Run with: go test -tags integration ./...
func TestE2E(t *testing.T) {
	fmt.Println("Starting e2e test")
	container := "velero"
	blob := "folder/test"
	testBody := "test text"

	tests := []struct {
		scenario string
		config   map[string]string
	}{
		{
			scenario: "GetProperties + ListKeys",
			config: map[string]string{
				storageAccountConfigKey:          "velerotest",
				storageAccountKeyEnvVarConfigKey: "AZ_STORAGE_KEY",
				resourceGroupConfigKey:           "saRgName",
				subscriptionIDConfigKey:          "81d18ba6-71e1-4858-a4a4-4c527ccdd4d6",
			},
		},
		{
			scenario: "GetProperties + ListKeys - AAD disabled",
			config: map[string]string{
				storageAccountConfigKey:          "velerotest",
				storageAccountKeyEnvVarConfigKey: "AZ_STORAGE_KEY",
				resourceGroupConfigKey:           "saRgName",
				subscriptionIDConfigKey:          "81d18ba6-71e1-4858-a4a4-4c527ccdd4d6",
				useAADConfigKey:                  "false",
			},
		},
		{
			scenario: "SA URI is provided - getProperties is not called, ListKeys is used.",
			config: map[string]string{
				storageAccountConfigKey:          "velerotest",
				storageAccountKeyEnvVarConfigKey: "AZ_STORAGE_KEY",
				resourceGroupConfigKey:           "saRgName",
				subscriptionIDConfigKey:          "81d18ba6-71e1-4858-a4a4-4c527ccdd4d6",
				storageAccountURIConfigKey:       "https://velerotest.blob.core.windows.net/",
			},
		},
		{
			scenario: "SA URI is provided - getProperties is not called, AAD is used",
			config: map[string]string{
				storageAccountConfigKey:          "velerotest",
				storageAccountKeyEnvVarConfigKey: "AZ_STORAGE_KEY",
				resourceGroupConfigKey:           "saRgName",
				subscriptionIDConfigKey:          "81d18ba6-71e1-4858-a4a4-4c527ccdd4d6",
				storageAccountURIConfigKey:       "https://velerotest.blob.core.windows.net/",
				useAADConfigKey:                  "true",
			},
		},
		{
			scenario: "AAD and SA URI is provided - getProperties is not called, custom AAD is used",
			config: map[string]string{
				storageAccountConfigKey:          		"velerotest",
				storageAccountKeyEnvVarConfigKey: 		"AZ_STORAGE_KEY",
				resourceGroupConfigKey:           		"saRgName",
				subscriptionIDConfigKey:          		"81d18ba6-71e1-4858-a4a4-4c527ccdd4d6",
				storageAccountURIConfigKey:           	"https://velerotest.blob.core.windows.net/",
				useAADConfigKey:                  	  	"true",
				activeDirectoryAuthorityURIConfigKey: 	"https://core.windows.net"
			},
		},
		{
			scenario: "GetProperties + ListKeys - AAD enabled",
			config: map[string]string{
				storageAccountConfigKey:          "velerotest",
				storageAccountKeyEnvVarConfigKey: "AZ_STORAGE_KEY",
				resourceGroupConfigKey:           "saRgName",
				subscriptionIDConfigKey:          "81d18ba6-71e1-4858-a4a4-4c527ccdd4d6",
				useAADConfigKey:                  "true",
			},
		},
	}

	for _, test := range tests {
		fmt.Println("=======================================")
		fmt.Println("Running test: ", test.scenario)
		config := test.config
		var log = &logrus.Logger{
			Out:       os.Stdout,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.DebugLevel,
		}

		store := &ObjectStore{log: log}
		err := store.Init(config)
		if err != nil {
			t.Fatal(err)
		}
		defer store.DeleteObject(container, blob)

		err = store.PutObject(container, blob, strings.NewReader(testBody))
		if err != nil {
			t.Fatal(err)
		}

		exists, err := store.ObjectExists(container, blob)
		if err != nil {
			t.Fatal(err)
		}
		require.True(t, exists)

		closer, err := store.GetObject(container, blob)
		if err != nil {
			t.Fatal(err)
		}
		body, err := ioutil.ReadAll(closer)
		if err != nil {
			t.Fatal(err)
		}
		require.Equal(t, testBody, string(body))

		objects, err := store.ListObjects(container, "fol")
		if err != nil {
			t.Fatal(err)
		}
		require.Equal(t, len(objects), 1)
		require.Equal(t, objects[0], blob)

		objects, err = store.ListObjects(container, "doesntexist")
		if err != nil {
			t.Fatal(err)
		}
		require.Equal(t, len(objects), 0)

		objects, err = store.ListCommonPrefixes(container, "fo", "/")
		if err != nil {
			t.Fatal(err)
		}
		require.Equal(t, len(objects), 1)
		require.Equal(t, objects[0], "folder/")

		url, err := store.CreateSignedURL(container, blob, 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("SAS URL: %s\n", url)
		body, err = downloadURL(url)
		if err != nil {
			t.Fatal(err)
		}
		require.Equal(t, testBody, string(body))

		err = store.DeleteObject(container, blob)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func downloadURL(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return responseBody, nil
}
