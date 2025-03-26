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
	"github.com/Microsoft/hcsshim/internal/gcs/prot"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/Microsoft/hcsshim/internal/protocol/guestrequest"
	"github.com/Microsoft/hcsshim/internal/protocol/guestresource"
	"github.com/Microsoft/hcsshim/internal/windevice"
	"github.com/Microsoft/hcsshim/pkg/cimfs"
	"github.com/Microsoft/hcsshim/pkg/securitypolicy"
	"github.com/pkg/errors"
)

const (
	sandboxStateDirName = "WcSandboxState"
	hivesDirName        = "Hives"
)

// - Handler functions handle the incoming message requests. It
// also enforces security policy for confidential cwcow containers.
// - These handler functions may do some additional processing before
// forwarding requests to inbox GCS or send responses back to hcsshim.
// - In case of any error encountered during processing, appropriate error
// messages are returned and responses are sent back to hcsshim from ListenAndServer().
// TODO (kiashok): Verbose logging is for WIP and will be removed eventually.
func (b *Bridge) createContainer(req *request) (err error) {
	ctx, span := oc.StartSpan(req.ctx, "sidecar::createContainer")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.ContainerCreate
	var containerConfig json.RawMessage
	r.ContainerConfig.Value = &containerConfig
	if err = json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcCreate")
	}

	// containerConfig can be of type uvnConfig or hcsschema.HostedSystem
	var (
		uvmConfig          prot.UvmConfig
		hostedSystemConfig hcsschema.HostedSystem
	)
	if err = json.Unmarshal(containerConfig, &uvmConfig); err == nil {
		systemType := uvmConfig.SystemType
		timeZoneInformation := uvmConfig.TimeZoneInformation
		log.G(ctx).Tracef("rpcCreate: uvmConfig: {systemType: %v, timeZoneInformation: %v}}", systemType, timeZoneInformation)
	} else if err = json.Unmarshal(containerConfig, &hostedSystemConfig); err == nil {
		schemaVersion := hostedSystemConfig.SchemaVersion
		container := hostedSystemConfig.Container
		log.G(ctx).Tracef("rpcCreate: HostedSystemConfig: {schemaVersion: %v, container: %v}}", schemaVersion, container)
	} else {
		return fmt.Errorf("Invalid request to rpcCreateContainer")
	}

	b.forwardRequestToGcs(req)
	return err
}

func (b *Bridge) startContainer(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::startContainer")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.RequestBase
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrapf(err, "failed to unmarshal rpcStart")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) shutdownGraceful(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::shutdownGraceful")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.RequestBase
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcShutdownGraceful")
	}

	// TODO (kiashok/Mahati): Since gcs-sidecar can be used for all types of windows
	// containers, it is important to check if we want to
	// enforce policy or not.
	b.hostState.securityPolicyEnforcer.EnforceShutdownContainerPolicy(req.ctx, r.ContainerID)
	if err != nil {
		return fmt.Errorf("rpcShudownGraceful operation not allowed: %v", err)
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) shutdownForced(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::shutdownForced")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.RequestBase
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcShutdownForced")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) executeProcess(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::executeProcess")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.ContainerExecuteProcess
	var processParamSettings json.RawMessage
	r.Settings.ProcessParameters.Value = &processParamSettings
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcExecuteProcess")
	}

	var processParams hcsschema.ProcessParameters
	if err := json.Unmarshal(processParamSettings, &processParams); err != nil {
		return errors.Wrap(err, "rpcExecProcess: invalid params type for request")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) waitForProcess(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::waitForProcess")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.ContainerWaitForProcess
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal waitForProcess")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) signalProcess(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::signalProcess")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.ContainerSignalProcess
	var rawOpts json.RawMessage
	r.Options = &rawOpts
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcSignalProcess")
	}

	var wcowOptions guestresource.SignalProcessOptionsWCOW
	if rawOpts != nil {
		if err := json.Unmarshal(rawOpts, &wcowOptions); err != nil {
			return errors.Wrap(err, "rpcSignalProcess: invalid Options type for request")
		}
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) resizeConsole(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::resizeConsole")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.ContainerResizeConsole
	if err := json.Unmarshal(req.message, &r); err != nil {
		return fmt.Errorf("failed to unmarshal rpcSignalProcess: %v", req)
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) getProperties(req *request) (err error) {
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

func (b *Bridge) negotiateProtocol(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::negotiateProtocol")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.NegotiateProtocolRequest
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcNegotiateProtocol")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) dumpStacks(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::dumpStacks")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.DumpStacksRequest
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcDumpStacks")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) deleteContainerState(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::deleteContainerState")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	var r prot.DeleteContainerStateRequest
	if err := json.Unmarshal(req.message, &r); err != nil {
		return errors.Wrap(err, "failed to unmarshal rpcDeleteContainer")
	}

	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) updateContainer(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::updateContainer")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	// No callers in the code for rpcUpdateContainer
	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) lifecycleNotification(req *request) (err error) {
	_, span := oc.StartSpan(req.ctx, "sidecar::lifecycleNotification")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	// No callers in the code for rpcLifecycleNotification
	b.forwardRequestToGcs(req)
	return nil
}

func (b *Bridge) modifySettings(req *request) (err error) {
	ctx, span := oc.StartSpan(req.ctx, "sidecar::modifySettings")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	log.G(ctx).Tracef("rpcModifySettings: MsgType: %v, Payload: %v", req.header.Type, string(req.message))
	modifyRequest, err := unmarshalContainerModifySettings(req)
	if err != nil {
		return err
	}
	modifyGuestSettingsRequest := modifyRequest.Request.(*guestrequest.ModificationRequest)
	guestResourceType := modifyGuestSettingsRequest.ResourceType
	guestRequestType := modifyGuestSettingsRequest.RequestType
	log.G(ctx).Tracef("rpcModifySettings: resourceType: %v, requestType: %v", guestResourceType, guestRequestType)

	switch guestRequestType {
	case guestrequest.RequestTypeAdd:
	case guestrequest.RequestTypeRemove:
	case guestrequest.RequestTypePreAdd:
	case guestrequest.RequestTypeUpdate:
	default:
		return fmt.Errorf("invald guestRequestType %v", guestRequestType)
	}

	if guestResourceType != "" {
		switch guestResourceType {
		case guestresource.ResourceTypeCombinedLayers:
			settings := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWCombinedLayers)
			log.G(ctx).Tracef("WCOWCombinedLayers: {%v}", settings)

		case guestresource.ResourceTypeNetworkNamespace:
			settings := modifyGuestSettingsRequest.Settings.(*hcn.HostComputeNamespace)
			log.G(ctx).Tracef("HostComputeNamespaces { %v}", settings)

		case guestresource.ResourceTypeNetwork:
			settings := modifyGuestSettingsRequest.Settings.(*guestrequest.NetworkModifyRequest)
			log.G(ctx).Tracef("NetworkModifyRequest { %v}", settings)

		case guestresource.ResourceTypeMappedVirtualDisk:
			wcowMappedVirtualDisk := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWMappedVirtualDisk)
			log.G(ctx).Tracef("wcowMappedVirtualDisk { %v}", wcowMappedVirtualDisk)

		case guestresource.ResourceTypeHvSocket:
			hvSocketAddress := modifyGuestSettingsRequest.Settings.(*hcsschema.HvSocketAddress)
			log.G(ctx).Tracef("hvSocketAddress { %v }", hvSocketAddress)

		case guestresource.ResourceTypeMappedDirectory:
			settings := modifyGuestSettingsRequest.Settings.(*hcsschema.MappedDirectory)
			log.G(ctx).Tracef("hcsschema.MappedDirectory { %v }", settings)

		case guestresource.ResourceTypeSecurityPolicy:
			securityPolicyRequest := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWConfidentialOptions)
			log.G(ctx).Tracef("WCOWConfidentialOptions: { %v}", securityPolicyRequest)
			_ = b.hostState.SetWCOWConfidentialUVMOptions( /*ctx, */ securityPolicyRequest)

			// Send response back to shim
			resp := &prot.ResponseBase{
				Result:     0, // 0 means success
				ActivityID: req.activityID,
			}
			err := b.sendResponseToShim(req.ctx, prot.RpcModifySettings, req.header.ID, resp)
			if err != nil {
				return errors.Wrap(err, "error sending response to hcsshim")
			}
			return nil

		case guestresource.ResourceTypeWCOWBlockCims:
			// This is request to mount the merged cim at given volumeGUID
			wcowBlockCimMounts := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWBlockCIMMounts)
			log.G(ctx).Tracef("WCOWBlockCIMMounts { %v}", wcowBlockCimMounts)

			// The block device takes some time to show up. Wait for a few seconds.
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
					return errors.Wrap(err, "err getting scsiDevPath")
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
					return errors.Wrap(err, "error mounting merged block cims")
				}
			} else {
				_, err := cimfs.Mount(filepath.Join(layerCIMs[0].BlockPath, layerCIMs[0].CimName), wcowBlockCimMounts.VolumeGuid, wcowBlockCimMounts.MountFlags)
				if err != nil {
					return errors.Wrap(err, "error mounting merged block cims")
				}
			}

			// Send response back to shim
			resp := &prot.ResponseBase{
				Result:     0, // 0 means success
				ActivityID: req.activityID,
			}
			err = b.sendResponseToShim(req.ctx, prot.RpcModifySettings, req.header.ID, resp)
			if err != nil {
				return errors.Wrap(err, "error sending response to hcsshim")
			}
			return nil

		case guestresource.ResourceTypeCWCOWCombinedLayers:
			settings := modifyGuestSettingsRequest.Settings.(*guestresource.CWCOWCombinedLayers)
			containerID := settings.ContainerID
			log.G(ctx).Tracef("CWCOWCombinedLayers:: ContainerID: %v, ContainerRootPath: %v, Layers: %v, ScratchPath: %v",
				containerID, settings.CombinedLayers.ContainerRootPath, settings.CombinedLayers.Layers, settings.CombinedLayers.ScratchPath)

			// check that this is not denied by policy
			// TODO: modify gcs-sidecar code to pass context across all calls
			// TODO: Update modifyCombinedLayers with verified CimFS API
			var ctx context.Context
			policy_err := modifyCombinedLayers(ctx, containerID, guestRequestType, settings.CombinedLayers, b.hostState.securityPolicyEnforcer)
			if policy_err != nil {
				return errors.Wrapf(policy_err, "CimFS layer mount is denied by policy: %v", settings)
			}

			// TODO: Update modifyCombinedLayers with verified CimFS API

			// The following two folders are expected to be present in the scratch.
			// But since we have just formatted the scratch we would need to
			// create them manually.
			sandboxStateDirectory := filepath.Join(settings.CombinedLayers.ContainerRootPath, sandboxStateDirName)
			err = os.Mkdir(sandboxStateDirectory, 0777)
			if err != nil {
				return errors.Wrap(err, "failed to create sandboxStateDirectory")
			}

			hivesDirectory := filepath.Join(settings.CombinedLayers.ContainerRootPath, hivesDirName)
			err = os.Mkdir(hivesDirectory, 0777)
			if err != nil {
				return errors.Wrap(err, "failed to create hivesDirectory")
			}

			// Reconstruct WCOWCombinedLayers{} req before forwarding to GCS
			// as GCS does not understand ResourceTypeCWCOWCombinedLayers
			modifyGuestSettingsRequest.ResourceType = guestresource.ResourceTypeCombinedLayers
			modifyGuestSettingsRequest.Settings = settings.CombinedLayers
			modifyRequest.Request = modifyGuestSettingsRequest
			buf, err := json.Marshal(modifyRequest)
			if err != nil {
				return errors.Wrap(err, "failed to marshal rpcModifySettings")
			}
			var newRequest request
			newRequest.header = req.header
			newRequest.header.Size = uint32(len(buf)) + prot.HdrSize
			newRequest.message = buf
			req = &newRequest

		case guestresource.ResourceTypeMappedVirtualDiskForContainerScratch:
			wcowMappedVirtualDisk := modifyGuestSettingsRequest.Settings.(*guestresource.WCOWMappedVirtualDisk)
			log.G(ctx).Tracef("ResourceTypeMappedVirtualDiskForContainerScratch: { %v }", wcowMappedVirtualDisk)

			var ctx context.Context
			policy_err := modifyMappedVirtualDisk(ctx, guestRequestType, wcowMappedVirtualDisk, b.hostState.securityPolicyEnforcer)
			if policy_err != nil {
				return errors.Wrapf(policy_err, "Mount device denied by policy %v", wcowMappedVirtualDisk)
			}

			// 1. TODO (kiashok/Mahati): Need to enforce policy before calling into fsFormatter
			// 2. Call fsFormatter to format the scratch disk.
			// This will return the volume path of the mounted scratch.
			// Scratch disk should be >= 30 GB for refs formatter to work.

			// fsFormatter understands only virtualDevObjectPathFormat. Therefore fetch the
			// disk number for the corresponding lun
			var diskNumber uint64
			// It could take a few seconds for the attached scsi disk
			// to show up inside the UVM. Therefore adding retry logic
			// with delay here.
			for try := 0; try < 5; try++ {
				time.Sleep(1 * time.Second)
				_, diskNumber, err = windevice.GetScsiDevicePathAndDiskNumberFromControllerLUN(req.ctx,
					0, /* Only one controller allowed in wcow hyperv */
					uint8(wcowMappedVirtualDisk.Lun))
				if err != nil {
					if try == 4 {
						// bail out
						return errors.Wrapf(err, "error getting diskNumber for LUN %d", wcowMappedVirtualDisk.Lun)
					}
					continue
				} else {
					log.G(ctx).Tracef("DiskNumber of lun %d is:  %d", wcowMappedVirtualDisk.Lun, diskNumber)
					break
				}
			}
			diskPath := fmt.Sprintf(fsformatter.VirtualDevObjectPathFormat, diskNumber)
			log.G(ctx).Tracef("diskPath: %v, diskNumber: %v ", diskPath, diskNumber)
			mountedVolumePath, err := fsformatter.InvokeFsFormatter(req.ctx, diskPath)
			if err != nil {
				return errors.Wrap(err, "failed to invoke refsFormatter")
			}
			log.G(ctx).Tracef("\n mountedVolumePath returned from InvokeFsFormatter: %v", mountedVolumePath)

			// Forward the req as is to inbox gcs and let it retreive the volume.
			// While forwarding request to inbox gcs, make sure to replace the
			// resourceType to ResourceTypeMappedVirtualDisk that inbox GCS
			// understands.
			modifyGuestSettingsRequest.ResourceType = guestresource.ResourceTypeMappedVirtualDisk
			modifyRequest.Request = modifyGuestSettingsRequest
			buf, err := json.Marshal(modifyRequest)
			if err != nil {
				return errors.Wrap(err, "failed to marshal WCOWMappedVirtualDisk")
			}
			var newRequest request
			newRequest.header = req.header
			newRequest.header.Size = uint32(len(buf)) + prot.HdrSize
			newRequest.message = buf
			req = &newRequest

		default:
			// Invalid request
			return fmt.Errorf("invald modifySettingsRequest: %v", guestResourceType)
		}
	}

	b.forwardRequestToGcs(req)
	return nil
}

func modifyMappedVirtualDisk(
	ctx context.Context,
	rt guestrequest.RequestType,
	mvd *guestresource.WCOWMappedVirtualDisk,
	securityPolicy securitypolicy.SecurityPolicyEnforcer,
) (err error) {
	switch rt {
	case guestrequest.RequestTypeAdd:
		// TODO: Modify and update this with verified Cims API
		return securityPolicy.EnforceDeviceMountPolicy(ctx, mvd.ContainerPath, "hash")
	case guestrequest.RequestTypeRemove:
		// TODO: Modify and update this with verified Cims API
		return securityPolicy.EnforceDeviceUnmountPolicy(ctx, mvd.ContainerPath)
	default:
		return newInvalidRequestTypeError(rt)
	}
}

func modifyCombinedLayers(
	ctx context.Context,
	containerID string,
	rt guestrequest.RequestType,
	cl guestresource.WCOWCombinedLayers,
	securityPolicy securitypolicy.SecurityPolicyEnforcer,
) (err error) {
	switch rt {
	case guestrequest.RequestTypeAdd:
		layerPaths := make([]string, len(cl.Layers))
		for i, layer := range cl.Layers {
			layerPaths[i] = layer.Path
		}
		return securityPolicy.EnforceOverlayMountPolicy(ctx, containerID, layerPaths, cl.ContainerRootPath)
	case guestrequest.RequestTypeRemove:
		return securityPolicy.EnforceOverlayUnmountPolicy(ctx, cl.ContainerRootPath)
	default:
		return newInvalidRequestTypeError(rt)
	}
}
