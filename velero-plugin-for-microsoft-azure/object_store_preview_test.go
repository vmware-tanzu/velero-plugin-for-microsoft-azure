package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"testing"
)

const (
	mockConfigPath = "../mock_config_object_store_preview"
)

var mockConfig map[string]string = make(map[string]string)

// mock config file schema
// {
// 	"resourceGroup": "",
// 	"storageAccount": "",
// 	"storageAccountKeyEnvVar": "",
// 	"subscriptionId": "",
// 	"containerName": "",
// 	"storageAccountKey": "",
// 	"testBlobName": "",
// }

func loadMockConfigfile(path string) (map[string]string, error) {
	var allowedKeys []string = []string{"resourceGroup",
		"storageAccount",
		"storageAccountKeyEnvVar",
		"subscriptionId",
	}

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

	for k, v := range config {
		mockConfig[k] = v
	}

	os.Setenv(config["storageAccountKeyEnvVar"], config["storageAccountKey"])

	for key := range config {
		for _, allowedKey := range allowedKeys {
			if key == allowedKey {
				goto SKIP
			}
		}
		delete(config, key)
	SKIP:
	}

	println(mockConfig)
	return config, nil
}

func TestPreviewInit(t *testing.T) {
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

func TestPreviewPutObject(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}

	fd, err := os.Open(mockConfig["testFilePath"])
	if err != nil {
		t.Error(err)
	}
	defer fd.Close()

	err = objectStore.PutObject(mockConfig["containerName"], mockConfig["testBlobName"], fd)
	if err != nil {
		t.Error(err)
	}
}

func TestPreviewListObjects(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}

	objects, err := objectStore.ListObjects(mockConfig["containerName"], "")
	if err != nil {
		t.Error(err)
	}
	if len(objects) == 0 {
		t.Error("No objects found")
	}
}

func TestPreviewObjectExists(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}

	exists, err := objectStore.ObjectExists(mockConfig["containerName"], mockConfig["testBlobName"])
	if err != nil {
		t.Error(err)
	}

	if !exists {
		t.Fail()
	}
}

func TestPreviewGetObject(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}
	rc, err := objectStore.GetObject(mockConfig["containerName"], mockConfig["testBlobName"])
	if err != nil {
		t.Error(err)
	}

	fd, err := os.OpenFile(mockConfig["testFilePath"]+"-output", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		t.Error(err)
	}

	bw, err := io.Copy(fd, rc)
	if err != nil {
		t.Error(err)
	}
	t.Logf("bytes written: %d", bw)
}
func TestPreviewDeleteObject(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}

	objectStore := ObjectStorePreview{}

	err = objectStore.Init(config)
	if err != nil {
		t.Error(err)
	}
	err = objectStore.DeleteObject(mockConfig["containerName"], mockConfig["testBlobName"])
	if err != nil {
		t.Error(err)
	}
}
