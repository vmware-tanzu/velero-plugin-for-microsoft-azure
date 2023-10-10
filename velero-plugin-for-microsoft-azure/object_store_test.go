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
	"io"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestObjectExists(t *testing.T) {
	tests := []struct {
		name           string
		getBlobError   error
		exists         bool
		errorResponse  error
		expectedExists bool
		expectedError  string
	}{
		{
			name:           "getBlob error",
			exists:         false,
			errorResponse:  errors.New("getBlob"),
			expectedExists: false,
			expectedError:  "getBlob",
		},
		{
			name:           "exists",
			exists:         true,
			errorResponse:  nil,
			expectedExists: true,
		},
		{
			name:           "doesn't exist",
			exists:         false,
			errorResponse:  nil,
			expectedExists: false,
		},
		{
			name:           "error checking for existence",
			exists:         false,
			errorResponse:  errors.New("bad"),
			expectedExists: false,
			expectedError:  "bad",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blobGetter := new(mockBlobGetter)
			defer blobGetter.AssertExpectations(t)

			o := &ObjectStore{
				blobGetter: blobGetter,
			}

			bucket := "b"
			key := "k"

			blob := new(mockBlob)
			defer blob.AssertExpectations(t)
			blobGetter.On("getBlob", bucket, key).Return(blob)

			blob.On("Exists").Return(tc.exists, tc.errorResponse)

			exists, err := o.ObjectExists(bucket, key)

			if tc.expectedError != "" {
				assert.EqualError(t, err, tc.expectedError)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tc.expectedExists, exists)
		})
	}
}

type mockBlobGetter struct {
	mock.Mock
}

func (m *mockBlobGetter) getBlob(bucket string, key string) blob {
	args := m.Called(bucket, key)
	return args.Get(0).(blob)
}

type mockBlob struct {
	mock.Mock
}

func (m *mockBlob) PutBlock(blockID string, chunk []byte, options *blockblob.StageBlockOptions) error {
	args := m.Called(blockID, chunk, options)
	return args.Error(0)
}
func (m *mockBlob) PutBlockList(blocks []string, options *blockblob.CommitBlockListOptions) error {
	args := m.Called(blocks, options)
	return args.Error(0)
}

func (m *mockBlob) Exists() (bool, error) {
	args := m.Called()
	return args.Bool(0), args.Error(1)
}

func (m *mockBlob) Get(options *azblob.DownloadStreamOptions) (io.ReadCloser, error) {
	args := m.Called(options)
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *mockBlob) Delete(options *azblob.DeleteBlobOptions) error {
	args := m.Called(options)
	return args.Error(0)
}

func (m *mockBlob) GetSASURI(ttl time.Duration, sharedKeyCredential *azblob.SharedKeyCredential) (string, error) {
	args := m.Called(ttl, sharedKeyCredential)
	return args.String(0), args.Error(1)
}

func TestGetBlockSize(t *testing.T) {
	logger := logrus.New()
	config := map[string]string{}
	// not specified
	size := getBlockSize(logger, config)
	assert.Equal(t, defaultBlockSize, size)

	// invalid value specified
	config[blockSizeConfigKey] = "invalid"
	size = getBlockSize(logger, config)
	assert.Equal(t, defaultBlockSize, size)

	// value < 0 specified
	config[blockSizeConfigKey] = "0"
	size = getBlockSize(logger, config)
	assert.Equal(t, defaultBlockSize, size)

	// value > max size specified
	config[blockSizeConfigKey] = "1048576000"
	size = getBlockSize(logger, config)
	assert.Equal(t, maxBlockSize, size)

	// valid value specified
	config[blockSizeConfigKey] = "1048570"
	size = getBlockSize(logger, config)
	assert.Equal(t, 1048570, size)
}
