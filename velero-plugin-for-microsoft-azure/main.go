/*
Copyright 2017, 2019 the Velero contributors.

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

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
)

func main() {
	var osh veleroplugin.HandlerInitializer
	override := strings.ToLower(os.Getenv("AZURE_OBJECT_STORE_OVERRIDE"))

	if override == "preview" {
		osh = newAzureObjectStore
	} else {
		osh = newObjectStorePreview
	}

	veleroplugin.NewServer().
		BindFlags(pflag.CommandLine).
		RegisterObjectStore("velero.io/azure", osh).
		RegisterVolumeSnapshotter("velero.io/azure", newAzureVolumeSnapshotter).
		Serve()
}

func newAzureObjectStore(logger logrus.FieldLogger) (interface{}, error) {
	return newObjectStore(logger), nil
}

func newAzureVolumeSnapshotter(logger logrus.FieldLogger) (interface{}, error) {
	return newVolumeSnapshotter(logger), nil
}
