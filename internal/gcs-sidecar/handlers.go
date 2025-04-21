//go:build windows
// +build windows

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/fsformatter"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/protocol/guestrequest"
	"github.com/Microsoft/hcsshim/internal/protocol/guestresource"
	"github.com/Microsoft/hcsshim/internal/windevice"
	"github.com/Microsoft/hcsshim/pkg/cimfs"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	sandboxStateDirName = "WcSandboxState"
	hivesDirName        = "Hives"
)

// - Current intent of these handler functions is to call the security policy
// enforcement code as needed.
// - These handler functions forward the message to inbox GCS or sends response
// back to hcsshim in cases where we do not need to forward message to
// inbox GCS for further processing.
// For example: ResourceTypeSecurityPolicy is something only the gcs-sidecar
// understands and need not be forwarded to inbox gcs.
// - In case of any error encountered during processing, appropriate error
// messages are returned and responses are sent back to hcsshim from caller, ListerAndServer().
func (b *Bridge) createContainer(req *request) error {
	var err error = nil
	var r containerCreate
	var containerConfig json.RawMessage

	r.ContainerConfig.Value = &containerConfig
	if err = json.Unmarshal(req.message, &r); err != nil {
		logrus.Errorf("failed to unmarshal rpcCreate: %v", req)
		return fmt.Errorf("failed to unmarshal rpcCreate: %v", req)
	}

	// containerCreate.ContainerConfig can be of type uvnConfig or hcsschema.HostedSystem
	var uvmConfig uvmConfig
	var hostedSystemConfig hcsschema.HostedSystem
	if err = json.Unmarshal(containerConfig, &uvmConfig); err == nil {
		systemType := uvmConfig.SystemType
		timeZoneInformation := uvmConfig.TimeZoneInformation
		logrus.Tracef("rpcCreate: \n ContainerCreate{ requestBase: %v, uvmConfig: {systemType: %v, timeZoneInformation: %v}}", r.requestBase, systemType, timeZoneInformation)
	} else if err = json.Unmarshal(containerConfig, &hostedSystemConfig); err == nil {
		schemaVersion := hostedSystemConfig.SchemaVersion
		container := hostedSystemConfig.Container
		logrus.Tracef("rpcCreate: \n ContainerCreate{ requestBase: %v, ContainerConfig: {schemaVersion: %v, container: %v}}", r.requestBase, schemaVersion, container)
	} else {
		logrus.Tracef("createContainer: invalid containerConfig type. Request: %v", req)
		return fmt.Errorf("createContainer: invalid containerConfig type. Request: %v", r)
	}

	b.forwardRequestToGcs(req)
	return err
}

func (b *Bridge) startContainer(req *request) error {
	var r requestBase
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcStart: %v", req)
	}
	logrus.Tracef("rpcStart: \n requestBase: %v", r)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) shutdownGraceful(req *request) error {
	var r requestBase
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcShutdownGraceful: %v", req)
	}
	logrus.Tracef("rpcShutdownGraceful: \n requestBase: %v", r)

	// Since gcs-sidecar can be used for all types of windows
	// containers, it is important to check if we want to
	// enforce policy or not.
	if b.hostState.isSecurityPolicyEnforcerInitialized() {
		err := b.hostState.securityPolicyEnforcer.EnforceShutdownContainerPolicy(req.ctx, r.ContainerID)
		if err != nil {
			return fmt.Errorf("rpcShudownGraceful operation not allowed: %v", err)
		}
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) shutdownForced(req *request) error {
	var r requestBase
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcShutdownForced: %v", req)
	}
	logrus.Tracef("rpcShutdownForced: \n requestBase: %v", r)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) executeProcess(req *request) error {
	var r containerExecuteProcess
	var processParamSettings json.RawMessage
	r.Settings.ProcessParameters.Value = &processParamSettings
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcExecuteProcess: %v", req)
	}
	containerID := r.requestBase.ContainerID
	stdioRelaySettings := r.Settings.StdioRelaySettings
	vsockStdioRelaySettings := r.Settings.VsockStdioRelaySettings

	var processParams hcsschema.ProcessParameters
	if err := json.Unmarshal(processParamSettings, &processParams); err != nil {
		logrus.Errorf("rpcExecProcess: invalid params type for request %v", r.Settings)
		return fmt.Errorf("rpcExecProcess: invalid params type for request %v", r.Settings)
	}
	logrus.Tracef("rpcExecProcess: \n containerID: %v, schema1.ProcessParameters{ params: %v, stdioRelaySettings: %v, vsockStdioRelaySettings: %v }", containerID, processParams, stdioRelaySettings, vsockStdioRelaySettings)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) waitForProcess(req *request) error {
	var r containerWaitForProcess
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal waitForProcess: %v", req)
	}
	logrus.Tracef("rpcWaitForProcess: \n containerWaitForProcess{ requestBase: %v, processID: %v, timeoutInMs: %v }", r.requestBase, r.ProcessID, r.TimeoutInMs)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) signalProcess(req *request) error {
	var r containerSignalProcess
	var rawOpts json.RawMessage
	r.Options = &rawOpts
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcSignalProcess: %v", req)
	}

	logrus.Tracef("rpcSignalProcess: request %v", r)

	var wcowOptions guestresource.SignalProcessOptionsWCOW
	if rawOpts != nil {
		if err := json.Unmarshal(rawOpts, &wcowOptions); err != nil {
			logrus.Errorf("rpcSignalProcess: invalid Options type for request %v", r)
			return fmt.Errorf("rpcSignalProcess: invalid Options type for request %v", r)
		}
	}
	logrus.Tracef("rpcSignalProcess: \n containerSignalProcess{ requestBase: %v, processID: %v, Options: %v }", r.requestBase, r.ProcessID, wcowOptions)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) resizeConsole(req *request) error {
	var r containerResizeConsole
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcSignalProcess: %v", req)
	}
	logrus.Tracef("rpcResizeConsole: \n containerResizeConsole{ requestBase: %v, processID: %v, height: %v, width: %v }", r.requestBase, r.ProcessID, r.Height, r.Width)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) getProperties(req *request) error {
	// TODO: This has containerGetProperties and containerGetPropertiesV2. Need to find a way to differentiate!
	/*
		var r containerGetProperties
		if err := json.Unmarshal(req.message, &r); err != nil {
			return fmt.Errorf("failed to unmarshal rpcSignalProcess: %v", req)
		}
	*/
	// TODO: Error out if v1 schema is being used as we will not support bringing up sidecar-gcs there
	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) negotiateProtocol(req *request) error {
	var r negotiateProtocolRequest
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcNegotiateProtocol: %v", req)
	}
	logrus.Tracef("rpcNegotiateProtocol: negotiateProtocolRequest{ requestBase %v, MinVersion: %v, MaxVersion: %v }", r.requestBase, r.MinimumVersion, r.MaximumVersion)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) dumpStacks(req *request) error {
	var r dumpStacksRequest
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcStart: %v", req)
	}

	if b.hostState.isSecurityPolicyEnforcerInitialized() {
		err := b.hostState.securityPolicyEnforcer.EnforceDumpStacksPolicy(req.ctx)
		if err != nil {
			return errors.Wrapf(err, "dump stacks denied due to policy")
		}
	}

	logrus.Tracef("rpcDumpStacks: \n requestBase: %v", r.requestBase)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) deleteContainerState(req *request) error {
	var r deleteContainerStateRequest
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcStart: %v", req)
	}
	logrus.Tracef("rpcDeleteContainerRequest: \n requestBase: %v", r.requestBase)

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) updateContainer(req *request) error {
	// No callers in the code for rpcUpdateContainer
	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) lifecycleNotification(req *request) error {
	// No callers in the code for rpcLifecycleNotification
	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) modifySettings(req *request) error {
	logrus.Tracef("modifySettings:Header {Type: %v Size: %v ID: %v }\n msg: %v \n", req.header.Type, req.header.Size, req.header.ID, string(req.message))
	modifyRequest, err := unmarshalContainerModifySettings(req)
	if err != nil {
		return err
	}
	modifyGuestSettingsRequest := modifyRequest.Request.(*guestrequest.ModificationRequest)
	guestResourceType := modifyGuestSettingsRequest.ResourceType
	guestRequestType := modifyGuestSettingsRequest.RequestType // add, remove, preadd, update
	logrus.Tracef("rpcModifySettings: guestRequest.ModificationRequest { resourceType: %v \n, requestType: %v", guestResourceType, guestRequestType)

	// TODO: Do we need to validate request types?
	switch guestRequestType {
	case guestrequest.RequestTypeAdd:
	case guestrequest.RequestTypeRemove:
	case guestrequest.RequestTypePreAdd:
	case guestrequest.RequestTypeUpdate:
	default:
		logrus.Errorf("\n Invald guestRequestType: %v", guestRequestType)
		return fmt.Errorf("invald guestRequestType %v", guestRequestType)
	}

	if guestResourceType != "" {
		switch guestResourceType {
		case guestresource.ResourceTypeCombinedLayers:
			settings := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWCombinedLayers)
			logrus.Tracef(", WCOWCombinedLayers {ContainerRootPath: %v, Layers: %v, ScratchPath: %v} \n", settings.ContainerRootPath, settings.Layers, settings.ScratchPath)

		case guestresource.ResourceTypeNetworkNamespace:
			settings := modifyGuestSettingsRequest.Settings.(*hcn.HostComputeNamespace)
			logrus.Tracef(", HostComputeNamespaces { %v} \n", settings)

		case guestresource.ResourceTypeNetwork:
			settings := modifyGuestSettingsRequest.Settings.(*guestrequest.NetworkModifyRequest)
			logrus.Tracef(", NetworkModifyRequest { %v} \n", settings)

		case guestresource.ResourceTypeMappedVirtualDisk:
			wcowMappedVirtualDisk := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWMappedVirtualDisk)
			// TODO: For verified cims (Cimfs API not ready yet)
			logrus.Tracef(", wcowMappedVirtualDisk { %v} \n", wcowMappedVirtualDisk)

		case guestresource.ResourceTypeHvSocket:
			logrus.Tracef("guestresource.ResourceTypeHvSocket \n")
			hvSocketAddress := modifyGuestSettingsRequest.Settings.(*hcsschema.HvSocketAddress)
			logrus.Tracef(", hvSocketAddress { %v} \n", hvSocketAddress)

		case guestresource.ResourceTypeMappedDirectory:
			logrus.Tracef("guestresource.ResourceTypeMappedDirectory \n")
			settings := modifyGuestSettingsRequest.Settings.(*hcsschema.MappedDirectory)
			logrus.Tracef(", hcsschema.MappedDirectory { %v } \n", settings)

		case guestresource.ResourceTypeSecurityPolicy:
			securityPolicyRequest := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWConfidentialOptions)
			logrus.Tracef(", WCOWConfidentialOptions: { %v} \n", securityPolicyRequest)
			_ = b.hostState.SetWCOWConfidentialUVMOptions( /*ctx, */ securityPolicyRequest)

			// Send response back to shim
			resp := &responseBase{
				Result:     0, // 0 means success
				ActivityID: req.activityID,
			}
			err := b.sendResponseToShim(rpcModifySettings, req.header.ID, resp)
			if err != nil {
				logrus.Tracef("error sending response to hcsshim: %v", err)
				return fmt.Errorf("error sending early reply back to hcsshim")
			}
			return nil

		case guestresource.ResourceTypeWCOWBlockCims:
			// This is request to mount the merged cim at given volumeGUID
			wcowBlockCimMounts := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWBlockCIMMounts)
			logrus.Tracef(", WCOWBlockCIMMounts { %v} \n", wcowBlockCimMounts)

			// The block device takes some time to show up, temporary hack
			time.Sleep(1 * time.Second)

			var layerCIMs []*cimfs.BlockCIM
			ctx := context.Background()
			for _, blockCimDevice := range wcowBlockCimMounts.BlockCIMs {
				// Get the scsi device path for the blockCim lun
				scsiDevPath, _, err := windevice.GetScsiDevicePathAndDiskNumberFromControllerLUN(
					ctx,
					0, /* controller is always 0 for wcow */
					uint8(blockCimDevice.Lun))
				if err != nil {
					logrus.Errorf("err getting scsiDevPath: %v", err)
					return err
				}
				layerCim := cimfs.BlockCIM{
					Type:      cimfs.BlockCIMTypeDevice,
					BlockPath: scsiDevPath,
					CimName:   blockCimDevice.CimName,
				}
				layerCIMs = append(layerCIMs, &layerCim)

			}
			if len(layerCIMs) > 1 {
				// Get the topmost merge CIM and invoke the MountMergedBlockCIMs
				_, err := cimfs.MountMergedBlockCIMs(layerCIMs[0], layerCIMs[1:], wcowBlockCimMounts.MountFlags, wcowBlockCimMounts.VolumeGuid)
				if err != nil {
					return fmt.Errorf("error mounting merged block cims: %v", err)
				}
			} else {
				_, err := cimfs.Mount(filepath.Join(layerCIMs[0].BlockPath, layerCIMs[0].CimName), wcowBlockCimMounts.VolumeGuid, wcowBlockCimMounts.MountFlags)
				if err != nil {
					return fmt.Errorf("error mounting merged block cims: %v", err)
				}
			}

			// Send response back to shim
			resp := &responseBase{
				Result:     0, // 0 means success
				ActivityID: req.activityID,
			}
			err = b.sendResponseToShim(rpcModifySettings, req.header.ID, resp)
			if err != nil {
				logrus.Errorf("error sending response to hcsshim: %v", err)
				return fmt.Errorf("error sending early reply back to hcsshim")
			}
			return nil

		case guestresource.ResourceTypeCWCOWCombinedLayers:
			settings := modifyGuestSettingsRequest.Settings.(*guestresource.CWCOWCombinedLayers)
			containerID := settings.ContainerID
			logrus.Tracef(", CWCOWCombinedLayers {ContainerID: %v {ContainerRootPath: %v, Layers: %v, ScratchPath: %v}} \n",
				containerID, settings.CombinedLayers.ContainerRootPath, settings.CombinedLayers.Layers, settings.CombinedLayers.ScratchPath)

			// TODO: Update modifyCombinedLayers with verified CimFS API

			// Reconstruct WCOWCombinedLayers{} and req before forwarding to GCS
			// as GCS does not understand containerID in CombinedLayers request
			modifyGuestSettingsRequest.ResourceType = guestresource.ResourceTypeCombinedLayers
			modifyGuestSettingsRequest.Settings = settings.CombinedLayers
			modifyRequest.Request = modifyGuestSettingsRequest
			buf, err := json.Marshal(modifyRequest)
			if err != nil {
				return fmt.Errorf("failed to marshal rpcModifySettings: %v", req)
			}

			// The following two folders are expected to be present in thr scratch.
			// But since we have just formatted the scratch we would need to
			// create them manually.
			sandboxStateDirectory := filepath.Join(settings.CombinedLayers.ContainerRootPath, sandboxStateDirName)
			err = os.Mkdir(sandboxStateDirectory, 0777)
			if err != nil {
				logrus.Errorf("unexpected error creating sandboxStateDirectory: %v", err)
				return fmt.Errorf("unexpected error sandboxStateDirectory: %v", err)
			}

			hivesDirectory := filepath.Join(settings.CombinedLayers.ContainerRootPath, hivesDirName)
			err = os.Mkdir(hivesDirectory, 0777)
			if err != nil {
				logrus.Errorf("unexpected error creating hivesDirectory: %v", err)
				return fmt.Errorf("unexpected error hivesDirectory: %v", err)
			}

			var newRequest request
			newRequest.header = req.header
			newRequest.header.Size = uint32(len(buf)) + hdrSize
			newRequest.message = buf
			req = &newRequest

		case guestresource.ResourceTypeMappedVirtualDiskForContainerScratch:
			wcowMappedVirtualDisk := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWMappedVirtualDisk)
			logrus.Tracef(", wcowMappedVirtualDisk { %v} \n", wcowMappedVirtualDisk)

			// 1.TODO: Need to enforce policy before calling into fsFormatter
			// 2. Then call fsFormatter to format the scratch disk.
			// This will return the volume path of the mounted scratch.
			// Scratch disk should be >= 30 GB for refs formatter to work.

			// fsFormatter understands only virtualDevObjectPathFormat. Therefore fetch the
			// disk number for the corresponding lun
			var diskNumber uint64
			// It could take a few seconds for the attached scsi disk
			// to show up inside the UVM. Therefore adding retry logic
			// with delay here.
			for i := 0; i < 5; i++ {
				time.Sleep(1 * time.Second)
				_, diskNumber, err = windevice.GetScsiDevicePathAndDiskNumberFromControllerLUN(req.ctx,
					0, /* Only one controller allowed in wcow hyperv */
					uint8(wcowMappedVirtualDisk.Lun))
				if err != nil {
					if i == 4 {
						// bail out
						logrus.Errorf("error getting diskNumber for LUN %d, err : %v", wcowMappedVirtualDisk.Lun, err)
						return fmt.Errorf("error getting diskNumber for LUN %d", wcowMappedVirtualDisk.Lun)
					}
					continue
				} else {
					logrus.Tracef("DiskNumber of lun %d is:  %d", wcowMappedVirtualDisk.Lun, diskNumber)
					break
				}
			}
			diskPath := fmt.Sprintf(fsformatter.VirtualDevObjectPathFormat, diskNumber)
			logrus.Tracef("\n diskPath: %v, diskNumber: %v ", diskPath, diskNumber)
			mountedVolumePath, err := windevice.InvokeFsFormatter(req.ctx, diskPath)
			if err != nil {
				logrus.Errorf("\n InvokeFsFormatter returned err: %v", err)
				return err
			}
			logrus.Tracef("\n mountedVolumePath returned from InvokeFsFormatter: %v", mountedVolumePath)

			// Just forward the req as is to inbox gcs and let it retreive the volume.
			// While forwarding request to inbox gcs, make sure to replace
			// the resourceType to ResourceTypeMappedVirtualDisk that it
			// understands.
			modifyGuestSettingsRequest.ResourceType = guestresource.ResourceTypeMappedVirtualDisk
			modifyRequest.Request = modifyGuestSettingsRequest
			buf, err := json.Marshal(modifyRequest)
			if err != nil {
				return fmt.Errorf("failed to marshal rpcModifySettings: %v", req)
			}

			var newRequest request
			newRequest.header = req.header
			newRequest.header.Size = uint32(len(buf)) + hdrSize
			newRequest.message = buf
			req = &newRequest

		default:
			// Invalid request
			logrus.Errorf("Invald modifySettingsRequest: %v", guestResourceType)
			return fmt.Errorf("invald modifySettingsRequest: %v", guestResourceType)
		}
	}

	b.forwardRequestToGcs(req)
	return nil
}
