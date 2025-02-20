package util

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"

	"github.com/vmware-tanzu/velero/pkg/util/azure"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func Test_getCloudConfiguration(t *testing.T) {
	http.DefaultClient = &http.Client{
		Transport: RoundTripperFunc(func (req *http.Request) (*http.Response, error) {
			var content any = nil
			if req.URL.Path == "/metadata/endpoints" {
				if req.Host == "management.customcloudapi.net" {
					content = []map[string]interface{}{
						{
							"authentication": map[string]interface{}{
								"loginEndpoint": "https://login.customcloudapi.net",
								"audiences": []string{
									"https://management.core.customcloudapi.net",
									"https://management.customcloudapi.net",
								},
							},
							"name": "AzureCustomCloud",
							"suffixes": map[string]interface{}{
								"storage": "core.customcloudapi.net",
							},
							"resourceManager": "https://management.customcloudapi.net",
						},
					}
				}
				if req.Host == "management.local.azurestack.external" {
					content = []map[string]interface{}{
						{
							"authentication": map[string]interface{}{
								"loginEndpoint": "https://adfs.local.azurestack.external/adfs",
								"audiences": []string{
									"https://management.adfs.azurestack.local/1234567890",
								},
							},
							"name": "AzureStack-User-1234567890",
							"suffixes": map[string]interface{}{
								"storage": "local.azurestack.external",
							},
						},
					}
				}
			}

			if content != nil {
				data, _ := json.Marshal(content)
				return &http.Response{
					StatusCode: http.StatusOK,
					Status: http.StatusText(http.StatusOK),
					ContentLength: int64(len(data)),
					Body: io.NopCloser(bytes.NewBuffer(data)),
				}, nil
			}

			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status: http.StatusText(http.StatusNotFound),
				ContentLength: 0,
			},  nil
		}),
	}

	publicCloudWithADURI := cloud.AzurePublic
	publicCloudWithADURI.ActiveDirectoryAuthorityHost = "https://example.com"
	// Represents a custom AzureCloud environment
	azureCustomCloud := cloud.Configuration{
		ActiveDirectoryAuthorityHost: "https://login.customcloudapi.net",
		Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
			"blob": {
				Endpoint: "core.customcloudapi.net",
			},
		},
	}
	// Represents an AzureStackCloud environment (using ADFS)
	azureStackCloud := cloud.Configuration{
		ActiveDirectoryAuthorityHost: "https://adfs.local.azurestack.external/adfs",
		Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
			"blob": {
				Endpoint: "local.azurestack.external",
			},
		},
	}

	cases := []struct {
		name string
		bslCfg map[string]string
		creds map[string]string
		err bool
		expected cloud.Configuration
		pretestFn func()
		postestFn func()
	}{
		{
			name:   "invalid cloud name",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "invalid",
			},
			err: true,
		},
		{
			name:   "null cloud name",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "",
			},
			err:      false,
			expected: cloud.AzurePublic,
		},
		{
			name:   "azure public cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZURECLOUD",
			},
			err:      false,
			expected: cloud.AzurePublic,
		},
		{
			name:   "azure public cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZUREPUBLICCLOUD",
			},
			err:      false,
			expected: cloud.AzurePublic,
		},
		{
			name:   "azure public cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "azurecloud",
			},
			err:      false,
			expected: cloud.AzurePublic,
		},
		{
			name:   "azure China cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZURECHINACLOUD",
			},
			err:      false,
			expected: cloud.AzureChina,
		},
		{
			name:   "azure US government cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZUREUSGOVERNMENT",
			},
			err:      false,
			expected: cloud.AzureGovernment,
		},
		{
			name:   "azure US government cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZUREUSGOVERNMENTCLOUD",
			},
			err:      false,
			expected: cloud.AzureGovernment,
		},
		{
			name: "AD authority URI provided",
			bslCfg: map[string]string{
				azure.BSLConfigActiveDirectoryAuthorityURI: "https://example.com",
			},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "",
			},
			err:      false,
			expected: publicCloudWithADURI,
		},
		{
			name: "azure custom cloud",
			bslCfg: map[string]string{},
			creds: map[string]string{
				resourceManagerEndpoint: "https://management.customcloudapi.net",
				azure.CredentialKeyCloudName: "AZURECUSTOMCLOUD",
			},
			err: false,
			expected: azureCustomCloud,
		},
		{
			name: "azure stack no configuration provided",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZURESTACKCLOUD",
			},
			err: true,
		},
		{
			name: "azure stack cloud resourceManagerEndpoint provided",
			bslCfg: map[string]string{},
			creds: map[string]string{
				resourceManagerEndpoint: "https://management.local.azurestack.external",
				// when using the ARM endpoint, the cloud name follows this pattern where the numbers match ARM_TENANT_ID
				azure.CredentialKeyCloudName: "AzureStack-User-1234567890",
			},
			err: false,
			expected: azureStackCloud,
		},
		{
			name: "azure stack cloud configuration file provided",
			bslCfg: map[string]string{},
			creds: map[string]string{
				azure.CredentialKeyCloudName: "AZURESTACKCLOUD",
			},
			err: false,
			expected: azureStackCloud,
			pretestFn: func() {
				_, filename, _, _ := runtime.Caller(0)
				os.Setenv(azclient.EnvironmentFilepathName, filepath.Join(filepath.Dir(filename), "testfiles/azurestackcloud.json"))
			},
			postestFn: func() {
				os.Setenv(azclient.EnvironmentFilepathName, "")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pretestFn != nil {
				c.pretestFn()
			}
			cfg, err := getCloudConfiguration(c.bslCfg, c.creds)
			require.Equal(t, c.err, err != nil)
			if !c.err {
				assert.Equal(t, c.expected, cfg)
			}
			if c.postestFn != nil {
				c.postestFn()
			}
		})
	}
}