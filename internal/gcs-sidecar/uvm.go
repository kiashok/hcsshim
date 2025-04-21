//go:build windows
// +build windows

package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/guest/commonutils"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/protocol/guestrequest"
	"github.com/Microsoft/hcsshim/internal/protocol/guestresource"
	"github.com/pkg/errors"
)

func newInvalidRequestTypeError(rt guestrequest.RequestType) error {
	return errors.Errorf("the RequestType %q is not supported", rt)
}

func unmarshalContainerModifySettings(req *request) (*containerModifySettings, error) {
	ctx := req.ctx
	var r containerModifySettings
	var requestRawSettings json.RawMessage
	r.Request = &requestRawSettings
	if err := commonutils.UnmarshalJSONWithHresult(req.message, &r); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rpcModifySettings: %v", req)
	}

	var modifyGuestSettingsRequest guestrequest.ModificationRequest
	var rawGuestRequest json.RawMessage
	modifyGuestSettingsRequest.Settings = &rawGuestRequest
	if err := commonutils.UnmarshalJSONWithHresult(requestRawSettings, &modifyGuestSettingsRequest); err != nil {
		log.G(ctx).Errorf("invalid rpcModifySettings ModificationRequest request %v", r)
		return nil, fmt.Errorf("invalid rpcModifySettings ModificationRequest request %v", r)
	}
	log.G(ctx).Tracef("rpcModifySettings: ModificationRequest %v\n", modifyGuestSettingsRequest)

	if modifyGuestSettingsRequest.RequestType == "" {
		modifyGuestSettingsRequest.RequestType = guestrequest.RequestTypeAdd
	}

	switch modifyGuestSettingsRequest.ResourceType {
	case guestresource.ResourceTypeCWCOWCombinedLayers:
		settings := &guestresource.CWCOWCombinedLayers{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, settings); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeCombinedLayers request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeCombinedLayers request %v", r)
		}
		modifyGuestSettingsRequest.Settings = settings

	case guestresource.ResourceTypeCombinedLayers:
		settings := &guestresource.WCOWCombinedLayers{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, settings); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeCombinedLayers request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeCombinedLayers request %v", r)
		}
		modifyGuestSettingsRequest.Settings = settings

	case guestresource.ResourceTypeNetworkNamespace:
		settings := &hcn.HostComputeNamespace{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, settings); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeNetworkNamespace request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeNetworkNamespace request %v", r)
		}
		modifyGuestSettingsRequest.Settings = settings

	case guestresource.ResourceTypeNetwork:
		// following valid only for osversion.Build() >= osversion.RS5
		// since Cwcow is available only for latest versions this is ok
		// TODO: Check if osversion >= rs5 else error out
		settings := &guestrequest.NetworkModifyRequest{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, settings); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeNetwork request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeNetwork request %v", r)
		}
		modifyGuestSettingsRequest.Settings = settings

	case guestresource.ResourceTypeMappedVirtualDisk:
		wcowMappedVirtualDisk := &guestresource.WCOWMappedVirtualDisk{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, wcowMappedVirtualDisk); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeMappedVirtualDisk request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeMappedVirtualDisk request %v", r)
		}
		modifyGuestSettingsRequest.Settings = wcowMappedVirtualDisk
		log.G(ctx).Tracef(", wcowMappedVirtualDisk { %v} \n", wcowMappedVirtualDisk)

	case guestresource.ResourceTypeHvSocket:
		hvSocketAddress := &hcsschema.HvSocketAddress{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, hvSocketAddress); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeHvSocket request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeHvSocket request %v", r)
		}
		modifyGuestSettingsRequest.Settings = hvSocketAddress

	case guestresource.ResourceTypeMappedDirectory:
		settings := &hcsschema.MappedDirectory{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, settings); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeMappedDirectory request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeMappedDirectory request %v", r)
		}
		modifyGuestSettingsRequest.Settings = settings

	case guestresource.ResourceTypeSecurityPolicy:
		securityPolicyRequest := &guestresource.WCOWConfidentialOptions{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, securityPolicyRequest); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeSecurityPolicy request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeSecurityPolicy request %v", r)
		}
		modifyGuestSettingsRequest.Settings = securityPolicyRequest

	case guestresource.ResourceTypeMappedVirtualDiskForContainerScratch:
		wcowMappedVirtualDisk := &guestresource.WCOWMappedVirtualDisk{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, wcowMappedVirtualDisk); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeMappedVirtualDisk request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeMappedVirtualDisk request %v", r)
		}
		modifyGuestSettingsRequest.Settings = wcowMappedVirtualDisk

	case guestresource.ResourceTypeWCOWBlockCims:
		wcowBlockCimMounts := &guestresource.WCOWBlockCIMMounts{}
		if err := commonutils.UnmarshalJSONWithHresult(rawGuestRequest, wcowBlockCimMounts); err != nil {
			log.G(ctx).Errorf("invalid ResourceTypeWCOWBlockCims request %v", r)
			return nil, fmt.Errorf("invalid ResourceTypeWCOWBlockCims request %v", r)
		}
		modifyGuestSettingsRequest.Settings = wcowBlockCimMounts

	default:
		// Invalid request
		log.G(ctx).Errorf("Invald modifySettingsRequest: %v", modifyGuestSettingsRequest.ResourceType)
		return nil, fmt.Errorf("invald modifySettingsRequest: %v", modifyGuestSettingsRequest.ResourceType)
	}
	r.Request = &modifyGuestSettingsRequest
	return &r, nil
}
