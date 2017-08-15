package qingcloud

// See https://docs.qingcloud.com/api/volume/index.html

import (
	"fmt"
	"strings"
	qcservice "github.com/yunify/qingcloud-sdk-go/service"
	qcclient "github.com/yunify/qingcloud-sdk-go/client"
	qcconfig "github.com/yunify/qingcloud-sdk-go/config"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/sets"
	"github.com/yunify/snips/publish/qingcloud-sdk-go/service"
)

const (
	//https://docs.qingcloud.com/api/volume/describe_volumes.html
	//High Performance
	VolumeTypeHP = VolumeType(0)
	//High Capacity
	VolumeTypeHC = VolumeType(2)
	//Super High Performance
	VolumeTypeSHP = VolumeType(3)

	DefaultVolumeType = VolumeTypeHP

	//DefaultMaxQingCloudVolumes is the limit for volumes attached to an instance.
	DefaultMaxQingCloudVolumes = 10
)

var (
	supportVolumeTypes = sets.NewString("0", "2", "3")
)

// VolumeOptions specifies capacity and type for a volume.
// See https://docs.qingcloud.com/api/volume/create_volumes.html
type VolumeOptions struct {
	CapacityGB int
	VolumeType VolumeType
	VolumeName string
}

// Volumes is an interface for managing cloud-provisioned volumes
type VolumeManager interface {
	// Attach the disk to the specified instance
	// Returns the device (e.g. /dev/sdb) where we attached the volume
	// It checks if volume is already attached to node and succeeds in that case.
	AttachVolume(volumeID string, instanceID string) (string, error)

	// Detach the disk from the specified instance
	DetachVolume(volumeID string, instanceID string) error

	// Create a volume with the specified options
	CreateVolume(volumeOptions *VolumeOptions) (volumeID string, err error)

	// Delete the specified volume
	// Returns true if the volume was deleted
	// If the was not found, returns (false, nil)
	DeleteVolume(volumeID string) (bool, error)

	// Check if the volume is already attached to the instance
	VolumeIsAttached(volumeID string, instanceID string) (bool, error)

	// Check if a list of volumes are attached to the node with the specified NodeName
	DisksAreAttached(volumeIDs []string, instanceID string) (map[string]bool, error)

	GetDefaultVolumeType() VolumeType
}

type volumeManager struct {
	instanceService      *qcservice.InstanceService
	volumeService        *qcservice.VolumeService
	jobService           *qcservice.JobService
	zone                 string
	defaultVolumeType	VolumeType
}

// newVolumeManager returns a new instance of QingCloudVolumeManager.
func newVolumeManager(qcConfigPath string) (VolumeManager, error) {
	qcConfig, err := qcconfig.NewDefault()
	if err != nil {
		return nil, err
	}
	if err = qcConfig.LoadConfigFromFilepath(qcConfigPath); err != nil {
		return nil, err
	}

	qcService, err := qcservice.Init(qcConfig)
	if err != nil {
		return nil, err
	}

	volumeService, err := qcService.Volume(qcConfig.Zone)
	if err != nil {
		return nil, err
	}
	jobService, err := qcService.Job(qcConfig.Zone)
	if err != nil {
		return nil, err
	}

	defaultVolumeType, err := autoDetectedVolumeType(qcConfig)
	if err != nil {
		return nil, err
	}

	qc := volumeManager{
		volumeService:        volumeService,
		jobService:           jobService,
		zone:                 qcConfig.Zone,
		defaultVolumeType:	defaultVolumeType,
	}

	glog.V(1).Infof("QingCloudVolumeManager init finish, zone: %v, defaultVolumeType: %v", qc.zone, qc.defaultVolumeType)

	return &qc, nil
}

// AttachVolume implements Volumes.AttachVolume
func (vm *volumeManager) AttachVolume(volumeID string, instanceID string) (string, error) {
	glog.V(4).Infof("AttachVolume(%v,%v) called", volumeID, instanceID)

	attached, err := vm.VolumeIsAttached(volumeID, instanceID)
	if err != nil {
		return "", err
	}

	if !attached {
		output, err := vm.volumeService.AttachVolumes(&qcservice.AttachVolumesInput{
			Volumes:[]*string{ &volumeID},
			Instance: &instanceID,
		})
		if err != nil {
			return "", err
		}
		jobID := *output.JobID
		err = qcclient.WaitJob(vm.jobService, jobID, operationWaitTimeout, waitInterval)
		if err != nil {
			return "", err
		}
	}

	output, err := vm.volumeService.DescribeVolumes(&qcservice.DescribeVolumesInput{
		Volumes: []*string{&volumeID},
	})
	if err != nil {
		return "", err
	}
	if len(output.VolumeSet) == 0 {
		return "", fmt.Errorf("volume '%v' miss after attach it", volumeID)
	}

	dev := output.VolumeSet[0].Instance.Device
	if dev == nil || *dev == "" {
		return "", fmt.Errorf("the device of volume '%v' is empty", volumeID)
	}

	return *dev, nil
}

// DetachVolume implements Volumes.DetachVolume
func (vm *volumeManager) DetachVolume(volumeID string, instanceID string) error {
	glog.V(4).Infof("DetachVolume(%v,%v) called", volumeID, instanceID)

	output, err := vm.volumeService.DetachVolumes(&qcservice.DetachVolumesInput{
		Volumes: []*string{&volumeID},
		Instance: &instanceID,
	})
	if err != nil {
		return err
	}
	jobID := *output.JobID
	err = qcclient.WaitJob(vm.jobService, jobID, operationWaitTimeout, waitInterval)
	return err
}

// CreateVolume implements Volumes.CreateVolume
func (vm *volumeManager) CreateVolume(volumeOptions *VolumeOptions) (string, error) {
	glog.V(4).Infof("CreateVolume(%v) called", volumeOptions)

	output, err := vm.volumeService.CreateVolumes(&qcservice.CreateVolumesInput{
		VolumeName: &volumeOptions.VolumeName,
		Size:       &volumeOptions.CapacityGB,
		VolumeType: service.Int(int(volumeOptions.VolumeType)),
	})
	if err != nil {
		return "", err
	}
	jobID := *output.JobID
	qcclient.WaitJob(vm.jobService, jobID, operationWaitTimeout, waitInterval)
	return *output.Volumes[0], nil
}

// DeleteVolume implements Volumes.DeleteVolume
func (vm *volumeManager) DeleteVolume(volumeID string) (bool, error) {
	glog.V(4).Infof("DeleteVolume(%v) called", volumeID)

	output, err := vm.volumeService.DeleteVolumes(&qcservice.DeleteVolumesInput{
		Volumes: []*string{&volumeID},
	})
	if err != nil {
		if strings.Index(err.Error(), "already been deleted") >= 0 {
			return false, nil
		}
		return false, err
	}

	jobID := *output.JobID
	qcclient.WaitJob(vm.jobService, jobID, operationWaitTimeout, waitInterval)

	return true, nil
}

// VolumeIsAttached implements Volumes.VolumeIsAttached
func (vm *volumeManager) VolumeIsAttached(volumeID string, instanceID string) (bool, error) {
	glog.V(4).Infof("VolumeIsAttached(%v,%v) called", volumeID, instanceID)

	output, err := vm.volumeService.DescribeVolumes(&qcservice.DescribeVolumesInput{
		Volumes: []*string{&volumeID},
	})
	if err != nil {
		return false, err
	}
	if len(output.VolumeSet) == 0 {
		return false, nil
	}

	return *output.VolumeSet[0].Instance.InstanceID == instanceID, nil
}

func (vm *volumeManager)  DisksAreAttached(volumeIDs []string, instanceID string) (map[string]bool, error){
	glog.V(4).Infof("DisksAreAttached(%v,%v) called", volumeIDs, instanceID)

	attached := make(map[string]bool)
	for _, volumeID := range volumeIDs {
		attached[volumeID] = false
	}
	output, err := vm.volumeService.DescribeVolumes(&qcservice.DescribeVolumesInput{
		Volumes: qcservice.StringSlice(volumeIDs),
	})
	if err != nil {
		return nil, err
	}
	for _, volume := range output.VolumeSet {
		if *volume.Instance.InstanceID == instanceID {
			attached[*volume.VolumeID] = true
		}
	}
	return attached, nil
}

func (vm *volumeManager) GetDefaultVolumeType() VolumeType {
	return vm.defaultVolumeType
}