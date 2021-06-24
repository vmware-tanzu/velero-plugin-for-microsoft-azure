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
var testBlob string

// mock config file schema
// {
// 	"resourceGroup": "",
// 	"storageAccount": "",
// 	"storageAccountKeyEnvVar": "",
// 	"storageAccountKey": "",
// 	"subscriptionId": "",
// 	"containerName": "",
// 	"testBlob": ""
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

	testBlob = config["testBlob"]
	delete(config, "testBlob")

	os.Setenv(config["storageAccountKeyEnvVar"], config["storageAccountKey"])
	delete(config, "storageAccountKey")

	return config, nil
}

func TestInit(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

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

func TestNewObjectExists(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}

	exists, err := objectStore.ObjectExists(containerName, testBlob)
	if err != nil {
		t.Error(err)
	}

	if !exists {
		t.Fail()
	}
}
