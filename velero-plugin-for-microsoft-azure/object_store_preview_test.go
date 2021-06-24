package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"
)

const (
	mockConfigPath = "../mock_config_object_store_preview"
)

var containerName string

// mock config file schema
// {
// 	"resourceGroup": "",
// 	"storageAccount": "",
// 	"storageAccountKeyEnvVar": "",
// 	"storageAccountKey": "",
// 	"subscriptionId": "",
// 	"containerName": ""
// }

func loadMockConfigfile(path string) (map[string]string, error) {

	buf, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	err = json.Unmarshal(buf, &data)
	if err != nil {
		return nil, err
	}

	config := make(map[string]string)
	for k, v := range data {
		config[k] = v.(string)
	}

	containerName = config["containerName"]
	delete(config, "containerName")

	os.Setenv(config["storageAccountKeyEnvVar"], config["storageAccountKey"])
	delete(config, "storageAccountKey")

	return config, nil
}

func clearEnvVars() {
	os.Setenv("AZURE_STORAGE_ACCOUNT_KEY", "")
}

func TestInit(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(clearEnvVars)

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}
}

func TestListObjects(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(clearEnvVars)

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}

	objects, err := objectStore.ListObjects(containerName, "")
	if err != nil {
		t.Error(err)
	}
	for _, o := range objects {
		t.Log(o)
	}
}
