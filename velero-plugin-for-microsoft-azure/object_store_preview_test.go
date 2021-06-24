package main

import (
	"encoding/json"
	"io/ioutil"
	"testing"
)

const (
	mockConfigPath = "../mock_config_object_store_preview"
)

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

	return config, nil
}

func TestObjectStorePreviewInit(t *testing.T) {
	config, err := loadMockConfigfile(mockConfigPath)
	if err != nil {
		t.Error(err)
	}
	objectStore := ObjectStorePreview{}

	objectStore.Init(config)
}
