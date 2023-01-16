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
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/stretchr/testify/assert"
)

func TestCloud(t *testing.T) {
	cloudConfig, err := cloudFromName("AzureChinaCloud")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "core.chinacloudapi.cn", cloudConfig.Services[Storage].Endpoint)
	assert.Equal(t, "https://management.chinacloudapi.cn", cloudConfig.Services[cloud.ResourceManager].Endpoint)
}

func TestCloudDefault(t *testing.T) {
	cloudConfig, err := cloudFromName("")
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "core.windows.net", cloudConfig.Services[Storage].Endpoint)
}
