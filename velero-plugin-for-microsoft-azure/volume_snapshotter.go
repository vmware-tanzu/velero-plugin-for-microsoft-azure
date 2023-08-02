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
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	azruntime "github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	uuid "github.com/gofrs/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	veleroplugin "github.com/vmware-tanzu/velero/pkg/plugin/framework"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/azuredisk-csi-driver/pkg/util"
)

const (
	resourceGroupEnvVar = "AZURE_RESOURCE_GROUP"

	apiTimeoutConfigKey                       = "apiTimeout"
	snapsIncrementalConfigKey                 = "incremental"
	snapsTagsConfigKey                        = "tags"
	snapsActiveDirectoryAuthorityURIConfigKey = "activeDirectoryAuthorityURI"

	snapshotsResource = "snapshots"
	disksResource     = "disks"

	diskCSIDriver = "disk.csi.azure.com"
	pollingDelay  = 5 * time.Second
)

type VolumeSnapshotter struct {
	log                logrus.FieldLogger
	disks              *armcompute.DisksClient
	snaps              *armcompute.SnapshotsClient
	disksSubscription  string
	snapsSubscription  string
	disksResourceGroup string
	snapsResourceGroup string
	snapsIncremental   *bool
	apiTimeout         time.Duration
	snapsTags          map[string]string
}

type snapshotIdentifier struct {
	subscription  string
	resourceGroup string
	name          string
}

func (si *snapshotIdentifier) String() string {
	return getComputeResourceName(si.subscription, si.resourceGroup, snapshotsResource, si.name)
}

func newVolumeSnapshotter(logger logrus.FieldLogger) *VolumeSnapshotter {
	return &VolumeSnapshotter{log: logger}
}

func (b *VolumeSnapshotter) Init(config map[string]string) error {
	if err := veleroplugin.ValidateVolumeSnapshotterConfigKeys(config,
		resourceGroupConfigKey,
		apiTimeoutConfigKey,
		subscriptionIDConfigKey,
		snapsIncrementalConfigKey,
		snapsTagsConfigKey,
		credentialsFileConfigKey,
	); err != nil {
		return err
	}

	credentialsFile, err := selectCredentialsFile(config)
	if err != nil {
		return err
	}
	if err := loadCredentialsIntoEnv(credentialsFile); err != nil {
		return err
	}

	// we need AZURE_SUBSCRIPTION_ID, AZURE_RESOURCE_GROUP
	envVars, err := getRequiredValues(os.Getenv, subscriptionIDEnvVar, resourceGroupEnvVar)
	if err != nil {
		return errors.Wrap(err, "unable to get all required environment variables")
	}

	// set a different subscriptionId for snapshots if specified
	snapshotsSubscriptionID := envVars[subscriptionIDEnvVar]
	if val := config[subscriptionIDConfigKey]; val != "" {
		// if subscription was set in config, it is required to also set the resource group
		if _, err := getRequiredValues(mapLookup(config), resourceGroupConfigKey); err != nil {
			return errors.Wrap(err, "resourceGroup not specified, but is a requirement when backing up to a different subscription")
		}
		snapshotsSubscriptionID = val
	}

	// Get Azure cloudConfig from AZURE_CLOUD_NAME, if it exists. If the env var does not
	// exist, cloudFromName will return AzurePublic.
	cloudConfig, err := cloudFromName(os.Getenv(cloudNameEnvVar))
	if err != nil {
		return errors.Wrap(err, "unable to parse azure cloud name environment variable")
	}

	// Update active directory authority host if it is set in the configuration
	if config[snapsActiveDirectoryAuthorityURIConfigKey] != "" {
		cloudConfig.ActiveDirectoryAuthorityHost = config[snapsActiveDirectoryAuthorityURIConfigKey]
	}

	// if config["apiTimeout"] is empty, default to 2m; otherwise, parse it
	var apiTimeout time.Duration
	if val := config[apiTimeoutConfigKey]; val == "" {
		apiTimeout = 2 * time.Minute
	} else {
		apiTimeout, err = time.ParseDuration(val)
		if err != nil {
			return errors.Wrapf(err, "unable to parse value %q for config key %q (expected a duration string)", val, apiTimeoutConfigKey)
		}
	}

	clientOptions := policy.ClientOptions{Cloud: cloudConfig}
	credential, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{ClientOptions: clientOptions})
	if err != nil {
		return errors.Wrap(err, "error getting credentials from environment")
	}

	// if config["snapsIncrementalConfigKey"] is empty, default to nil; otherwise, parse it
	var snapshotsIncremental *bool
	if val := config[snapsIncrementalConfigKey]; val != "" {
		parseIncremental, err := strconv.ParseBool(val)
		if err != nil {
			return errors.Wrapf(err, "unable to parse value %q for config key %q (expected a boolean value)", val, snapsIncrementalConfigKey)
		}
		snapshotsIncremental = &parseIncremental
	} else {
		snapshotsIncremental = nil
	}

	var snapsTags map[string]string
	if val := config[snapsTagsConfigKey]; val != "" {
		snapsTags, err = util.ConvertTagsToMap(val)
		if err != nil {
			return errors.Wrapf(err, "unable to parse value %q for config key %q (the valid format is \"key1=value1,key2=value2\")", val, snapsTagsConfigKey)
		}
	}

	// set up clients
	disksClient, err := armcompute.NewDisksClient(envVars[subscriptionIDEnvVar], credential, &arm.ClientOptions{ClientOptions: clientOptions})
	if err != nil {
		return errors.Wrap(err, "error creating disk client")
	}
	snapsClient, err := armcompute.NewSnapshotsClient(snapshotsSubscriptionID, credential, &arm.ClientOptions{ClientOptions: clientOptions})
	if err != nil {
		return errors.Wrap(err, "error creating snapshot client")
	}

	b.disks = disksClient
	b.snaps = snapsClient
	b.disksSubscription = envVars[subscriptionIDEnvVar]
	b.snapsSubscription = snapshotsSubscriptionID
	b.disksResourceGroup = envVars[resourceGroupEnvVar]
	b.snapsResourceGroup = config[resourceGroupConfigKey]

	// if no resource group was explicitly specified in 'config',
	// use the value from the env var (i.e. the same one as where
	// the cluster & disks are)
	if b.snapsResourceGroup == "" {
		b.snapsResourceGroup = envVars[resourceGroupEnvVar]
	}

	b.apiTimeout = apiTimeout

	b.snapsIncremental = snapshotsIncremental

	b.snapsTags = snapsTags

	return nil
}

func (b *VolumeSnapshotter) CreateVolumeFromSnapshot(snapshotID, volumeType, volumeAZ string, iops *int64) (string, error) {
	snapshotIdentifier, err := parseFullSnapshotName(snapshotID)
	diskStorageAccountType := armcompute.DiskStorageAccountTypes(volumeType)
	if err != nil {
		return "", err
	}

	// Lookup snapshot info for its Location & Tags so we can apply them to the volume
	snapshotInfo, err := b.snaps.Get(context.TODO(), snapshotIdentifier.resourceGroup, snapshotIdentifier.name, nil)
	if err != nil {
		return "", errors.WithStack(err)
	}

	uid, err := uuid.NewV4()
	if err != nil {
		return "", errors.WithStack(err)
	}
	diskName := "restore-" + uid.String()

	disk := armcompute.Disk{
		Name:     &diskName,
		Location: snapshotInfo.Location,
		Properties: &armcompute.DiskProperties{
			CreationData: &armcompute.CreationData{
				CreateOption:     to.Ptr(armcompute.DiskCreateOptionCopy),
				SourceResourceID: to.Ptr(snapshotIdentifier.String()),
			},
		},
		SKU: &armcompute.DiskSKU{
			Name: to.Ptr(diskStorageAccountType),
		},
		Tags: snapshotInfo.Tags,
	}
	// If not a volume type 'zone redundant storage' restore the disk in the correct zone
	if diskStorageAccountType != armcompute.DiskStorageAccountTypesPremiumZRS && diskStorageAccountType != armcompute.DiskStorageAccountTypesStandardSSDZRS {
		regionParts := strings.Split(volumeAZ, "-")
		if len(regionParts) >= 2 {
			disk.Zones = []*string{to.Ptr(regionParts[len(regionParts)-1])}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.apiTimeout)
	defer cancel()

	pollerResp, err := b.disks.BeginCreateOrUpdate(ctx, b.disksResourceGroup, *disk.Name, disk, nil)
	if err != nil {
		return "", errors.WithStack(err)
	}
	_, err = pollerResp.PollUntilDone(ctx, &azruntime.PollUntilDoneOptions{Frequency: pollingDelay})
	if err != nil {
		return "", errors.WithStack(err)
	}
	return diskName, nil
}

func (b *VolumeSnapshotter) GetVolumeInfo(volumeID, volumeAZ string) (string, *int64, error) {
	res, err := b.disks.Get(context.TODO(), b.disksResourceGroup, volumeID, nil)
	if err != nil {
		return "", nil, errors.WithStack(err)
	}

	if res.SKU == nil {
		return "", nil, errors.New("disk has a nil SKU")
	}

	return string(*res.SKU.Name), nil, nil
}

func (b *VolumeSnapshotter) CreateSnapshot(volumeID, volumeAZ string, tags map[string]string) (string, error) {
	// Lookup disk info for its Location
	diskInfo, err := b.disks.Get(context.TODO(), b.disksResourceGroup, volumeID, nil)
	if err != nil {
		return "", errors.WithStack(err)
	}

	fullDiskName := getComputeResourceName(b.disksSubscription, b.disksResourceGroup, disksResource, volumeID)
	// snapshot names must be <= 80 characters long
	var snapshotName string
	uid, err := uuid.NewV4()
	if err != nil {
		return "", errors.WithStack(err)
	}
	suffix := "-" + uid.String()

	if len(volumeID) <= (80 - len(suffix)) {
		snapshotName = volumeID + suffix
	} else {
		snapshotName = volumeID[0:80-len(suffix)] + suffix
	}

	snap := armcompute.Snapshot{
		Name: &snapshotName,
		Properties: &armcompute.SnapshotProperties{
			CreationData: &armcompute.CreationData{
				CreateOption:     to.Ptr(armcompute.DiskCreateOptionCopy),
				SourceResourceID: &fullDiskName,
			},
			Incremental: b.snapsIncremental,
		},
		Tags:     getSnapshotTags(tags, b.snapsTags, diskInfo.Tags),
		Location: diskInfo.Location,
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.apiTimeout)
	defer cancel()

	pollerResp, err := b.snaps.BeginCreateOrUpdate(ctx, b.snapsResourceGroup, *snap.Name, snap, nil)
	if err != nil {
		return "", errors.WithStack(err)
	}
	_, err = pollerResp.PollUntilDone(ctx, &azruntime.PollUntilDoneOptions{Frequency: pollingDelay})
	if err != nil {
		return "", errors.WithStack(err)
	}
	return getComputeResourceName(b.snapsSubscription, b.snapsResourceGroup, snapshotsResource, snapshotName), nil
}

func getSnapshotTags(veleroTags, snapsTags map[string]string, diskTags map[string]*string) map[string]*string {
	if diskTags == nil && len(veleroTags) == 0 && len(snapsTags) == 0 {
		return nil
	}

	snapshotTags := make(map[string]*string)

	// copy tags from disk to snapshot
	for k, v := range diskTags {
		snapshotTags[k] = stringPtr(*v)
	}

	// merge Velero-assigned tags with the disk's tags (note that we want current
	// Velero-assigned tags to overwrite any older versions of them that may exist
	// due to prior snapshots/restores)
	for k, v := range veleroTags {
		// Azure does not allow slashes in tag keys, so replace
		// with dash (inline with what Kubernetes does)
		key := strings.Replace(k, "/", "-", -1)
		snapshotTags[key] = stringPtr(v)
	}

	for k, v := range snapsTags {
		snapshotTags[k] = stringPtr(v)
	}

	return snapshotTags
}

func stringPtr(s string) *string {
	return &s
}

func (b *VolumeSnapshotter) DeleteSnapshot(snapshotID string) error {
	snapshotInfo, err := parseFullSnapshotName(snapshotID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.apiTimeout)
	defer cancel()

	// we don't want to return an error if the snapshot doesn't exist, and
	// the Delete(..) call does not return a clear error if that's the case,
	// so first try to get it and return early if we get a 404.
	_, err = b.snaps.Get(ctx, snapshotInfo.resourceGroup, snapshotInfo.name, nil)
	if azureErr, ok := err.(*azcore.ResponseError); ok && azureErr.StatusCode == http.StatusNotFound {
		b.log.WithField("snapshotID", snapshotID).Debug("Snapshot not found")
		return nil
	}

	pollerResp, err := b.snaps.BeginDelete(ctx, snapshotInfo.resourceGroup, snapshotInfo.name, nil)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = pollerResp.PollUntilDone(ctx, &azruntime.PollUntilDoneOptions{Frequency: pollingDelay})
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func getComputeResourceName(subscription, resourceGroup, resource, name string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/%s/%s", subscription, resourceGroup, resource, name)
}

var (
	snapshotURIRegexp = regexp.MustCompile(
		`^\/subscriptions\/(?P<subscription>.*)\/resourceGroups\/(?P<resourceGroup>.*)\/providers\/Microsoft.Compute\/snapshots\/(?P<snapshotName>.*)$`)
	diskURIRegexp = regexp.MustCompile(`\/Microsoft.Compute\/disks\/.*$`)
)

// parseFullSnapshotName takes a fully-qualified snapshot name and returns
// a snapshot identifier or an error if the snapshot name does not match the
// regexp.
func parseFullSnapshotName(name string) (*snapshotIdentifier, error) {
	submatches := snapshotURIRegexp.FindStringSubmatch(name)
	if len(submatches) != len(snapshotURIRegexp.SubexpNames()) {
		return nil, errors.New("snapshot URI could not be parsed")
	}

	snapshotID := &snapshotIdentifier{}

	// capture names start at index 1 to line up with the corresponding indexes
	// of submatches (see godoc on SubexpNames())
	for i, names := 1, snapshotURIRegexp.SubexpNames(); i < len(names); i++ {
		switch names[i] {
		case "subscription":
			snapshotID.subscription = submatches[i]
		case "resourceGroup":
			snapshotID.resourceGroup = submatches[i]
		case "snapshotName":
			snapshotID.name = submatches[i]
		default:
			return nil, errors.New("unexpected named capture from snapshot URI regex")
		}
	}

	return snapshotID, nil
}

func (b *VolumeSnapshotter) GetVolumeID(unstructuredPV runtime.Unstructured) (string, error) {
	pv := new(v1.PersistentVolume)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredPV.UnstructuredContent(), pv); err != nil {
		return "", errors.WithStack(err)
	}

	if pv.Spec.CSI != nil {
		if pv.Spec.CSI.Driver == diskCSIDriver {
			return strings.TrimPrefix(diskURIRegexp.FindString(pv.Spec.CSI.VolumeHandle), "/Microsoft.Compute/disks/"), nil
		}
		b.log.Infof("Unable to handle CSI driver: %s", pv.Spec.CSI.Driver)
	}

	if pv.Spec.AzureDisk == nil {
		return "", nil
	}

	if pv.Spec.AzureDisk.DiskName == "" {
		return "", errors.New("spec.azureDisk.diskName not found")
	}

	return pv.Spec.AzureDisk.DiskName, nil
}

func (b *VolumeSnapshotter) SetVolumeID(unstructuredPV runtime.Unstructured, volumeID string) (runtime.Unstructured, error) {
	pv := new(v1.PersistentVolume)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredPV.UnstructuredContent(), pv); err != nil {
		return nil, errors.WithStack(err)
	}

	if pv.Spec.CSI != nil {
		if pv.Spec.CSI.Driver == diskCSIDriver {
			pv.Spec.CSI.VolumeHandle = getComputeResourceName(b.disksSubscription, b.disksResourceGroup, disksResource, volumeID)
		} else {
			return nil, fmt.Errorf("unable to handle CSI driver: %s", pv.Spec.CSI.Driver)
		}

	} else if pv.Spec.AzureDisk != nil {
		pv.Spec.AzureDisk.DiskName = volumeID
		pv.Spec.AzureDisk.DataDiskURI = getComputeResourceName(b.disksSubscription, b.disksResourceGroup, disksResource, volumeID)
	} else {
		return nil, errors.New("spec.csi and spec.azureDisk not found")
	}

	res, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pv)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &unstructured.Unstructured{Object: res}, nil
}
