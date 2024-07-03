//go:build windows
// +build windows

package hcsoci

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/coreinfo"
	"github.com/Microsoft/hcsshim/internal/cow"
	"github.com/Microsoft/hcsshim/internal/guestpath"
	"github.com/Microsoft/hcsshim/internal/hcs"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/jobobject"
	"github.com/Microsoft/hcsshim/internal/layers"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/oci"
	"github.com/Microsoft/hcsshim/internal/resources"
	"github.com/Microsoft/hcsshim/internal/schemaversion"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/Microsoft/hcsshim/internal/winapi"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

var (
	lcowRootInUVM = guestpath.LCOWRootPrefixInUVM + "/%s"
	wcowRootInUVM = guestpath.WCOWRootPrefixInUVM + "/%s"
)

// CreateOptions are the set of fields used to call CreateContainer().
// Note: In the spec, the LayerFolders must be arranged in the same way in which
// moby configures them: layern, layern-1,...,layer2,layer1,scratch
// where layer1 is the base read-only layer, layern is the top-most read-only
// layer, and scratch is the RW layer. This is for historical reasons only.
type CreateOptions struct {
	// Common parameters
	ID               string             // Identifier for the container
	Owner            string             // Specifies the owner. Defaults to executable name.
	Spec             *specs.Spec        // Definition of the container or utility VM being created
	SchemaVersion    *hcsschema.Version // Requested Schema Version. Defaults to v2 for RS5, v1 for RS1..RS4
	HostingSystem    *uvm.UtilityVM     // Utility or service VM in which the container is to be created.
	NetworkNamespace string             // Host network namespace to use (overrides anything in the spec)
	LCOWLayers       *layers.LCOWLayers

	// This is an advanced debugging parameter. It allows for diagnosability by leaving a containers
	// resources allocated in case of a failure. Thus you would be able to use tools such as hcsdiag
	// to look at the state of a utility VM to see what resources were allocated. Obviously the caller
	// must a) not tear down the utility VM on failure (or pause in some way) and b) is responsible for
	// performing the ReleaseResources() call themselves.
	DoNotReleaseResourcesOnFailure bool

	// ScaleCPULimitsToSandbox indicates that the container CPU limits should be adjusted to account
	// for the difference in CPU count between the host and the UVM.
	ScaleCPULimitsToSandbox bool
}

// createOptionsInternal is the set of user-supplied create options, but includes internal
// fields for processing the request once user-supplied stuff has been validated.
type createOptionsInternal struct {
	*CreateOptions

	actualSchemaVersion    *hcsschema.Version // Calculated based on Windows build and optional caller-supplied override
	actualID               string             // Identifier for the container
	actualOwner            string             // Owner for the container
	actualNetworkNamespace string
	ccgState               *hcsschema.ContainerCredentialGuardState // Container Credential Guard information to be attached to HCS container document

	windowsAdditionalMounts []hcsschema.MappedDirectory // Holds additional mounts based on added devices (such as SCSI). Only used for Windows v2 schema containers.
}

func validateContainerConfig(ctx context.Context, coi *createOptionsInternal) error {
	// check if gMSA is disabled
	if coi.Spec.Windows != nil {
		disableGMSA := oci.ParseAnnotationsDisableGMSA(ctx, coi.Spec)
		if _, ok := coi.Spec.Windows.CredentialSpec.(string); ok && disableGMSA {
			return fmt.Errorf("gMSA credentials are disabled: %w", hcs.ErrOperationDenied)
		}
	}

	return nil
}

func initializeCreateOptions(ctx context.Context, createOptions *CreateOptions) (*createOptionsInternal, error) {
	coi := &createOptionsInternal{
		CreateOptions: createOptions,
		actualID:      createOptions.ID,
		actualOwner:   createOptions.Owner,
	}

	if coi.Spec == nil {
		return nil, fmt.Errorf("spec must be supplied")
	}

	// Defaults if omitted by caller.
	if coi.actualID == "" {
		g, err := guid.NewV4()
		if err != nil {
			return nil, err
		}
		coi.actualID = g.String()
	}
	if coi.actualOwner == "" {
		coi.actualOwner = filepath.Base(os.Args[0])
	}

	if coi.HostingSystem != nil {
		// By definition, a hosting system can only be supplied for a v2 Xenon.
		coi.actualSchemaVersion = schemaversion.SchemaV21()
	} else {
		coi.actualSchemaVersion = schemaversion.DetermineSchemaVersion(coi.SchemaVersion)
	}

	log.G(ctx).WithFields(logrus.Fields{
		"options": fmt.Sprintf("%+v", createOptions),
		"schema":  coi.actualSchemaVersion,
	}).Debug("hcsshim::initializeCreateOptions")

	return coi, nil
}

// configureSandboxNetwork creates a new network namespace for the pod (sandbox)
// if required and then adds that namespace to the pod.
func configureSandboxNetwork(ctx context.Context, coi *createOptionsInternal, r *resources.Resources, ct oci.KubernetesContainerType) error {
	if coi.NetworkNamespace != "" {
		r.SetNetNS(coi.NetworkNamespace)
	} else {
		err := createNetworkNamespace(ctx, coi, r)
		if err != nil {
			return err
		}
	}
	coi.actualNetworkNamespace = r.NetNS()

	if coi.HostingSystem != nil {
		// Only add the network namespace to a standalone or sandbox
		// container but not a workload container in a sandbox that inherits
		// the namespace.
		if ct == oci.KubernetesContainerTypeNone || ct == oci.KubernetesContainerTypeSandbox {
			if err := coi.HostingSystem.ConfigureNetworking(ctx, coi.actualNetworkNamespace); err != nil {
				// No network setup type was specified for this UVM. Create and assign one here unless
				// we received a different error.
				if errors.Is(err, uvm.ErrNoNetworkSetup) {
					if err := coi.HostingSystem.CreateAndAssignNetworkSetup(ctx, "", ""); err != nil {
						return err
					}
					if err := coi.HostingSystem.ConfigureNetworking(ctx, coi.actualNetworkNamespace); err != nil {
						return err
					}
				} else {
					return err
				}
			}
			r.SetAddedNetNSToVM(true)
		}
	}

	return nil
}

// CreateContainer creates a container. It can cope with a  wide variety of
// scenarios, including v1 HCS schema calls, as well as more complex v2 HCS schema
// calls. Note we always return the resources that have been allocated, even in the
// case of an error. This provides support for the debugging option not to
// release the resources on failure, so that the client can make the necessary
// call to release resources that have been allocated as part of calling this function.
func CreateContainer(ctx context.Context, createOptions *CreateOptions) (_ cow.Container, _ *resources.Resources, err error) {
	coi, err := initializeCreateOptions(ctx, createOptions)
	if err != nil {
		return nil, nil, err
	}

	if err := validateContainerConfig(ctx, coi); err != nil {
		return nil, nil, fmt.Errorf("container config validation failed: %w", err)
	}

	r := resources.NewContainerResources(coi.ID)
	defer func() {
		if err != nil {
			if !coi.DoNotReleaseResourcesOnFailure {
				_ = resources.ReleaseResources(ctx, r, coi.HostingSystem, true)
			}
		}
	}()

	if coi.HostingSystem != nil {
		if coi.Spec.Linux != nil {
			r.SetContainerRootInUVM(fmt.Sprintf(lcowRootInUVM, coi.ID))
		} else {
			n := coi.HostingSystem.ContainerCounter()
			r.SetContainerRootInUVM(fmt.Sprintf(wcowRootInUVM, strconv.FormatUint(n, 16)))
		}
		// install kernel drivers if necessary.
		// do this before network setup in case any of the drivers requested are
		// network drivers
		driverClosers, err := addSpecGuestDrivers(ctx, coi.HostingSystem, coi.Spec.Annotations)
		if err != nil {
			return nil, r, err
		}
		r.Add(driverClosers...)
	}

	ct, _, err := oci.GetSandboxTypeAndID(coi.Spec.Annotations)
	if err != nil {
		return nil, r, err
	}
	isSandbox := ct == oci.KubernetesContainerTypeSandbox

	// Create a network namespace if necessary.
	if coi.Spec.Windows != nil &&
		coi.Spec.Windows.Network != nil &&
		schemaversion.IsV21(coi.actualSchemaVersion) {
		err = configureSandboxNetwork(ctx, coi, r, ct)
		if err != nil {
			return nil, r, fmt.Errorf("failure while creating namespace for container: %w", err)
		}
	}

	var hcsDocument, gcsDocument interface{}
	log.G(ctx).Debug("hcsshim::CreateContainer allocating resources")
	if coi.Spec.Linux != nil {
		if schemaversion.IsV10(coi.actualSchemaVersion) {
			return nil, r, errors.New("LCOW v1 not supported")
		}
		log.G(ctx).Debug("hcsshim::CreateContainer allocateLinuxResources")
		err = allocateLinuxResources(ctx, coi, r, isSandbox)
		if err != nil {
			log.G(ctx).WithError(err).Debug("failed to allocateLinuxResources")
			return nil, r, err
		}
		gcsDocument, err = createLinuxContainerDocument(ctx, coi, r.ContainerRootInUVM(), r.LcowScratchPath())
		if err != nil {
			log.G(ctx).WithError(err).Debug("failed createHCSContainerDocument")
			return nil, r, err
		}
	} else {
		err = allocateWindowsResources(ctx, coi, r, isSandbox)
		if err != nil {
			log.G(ctx).WithError(err).Debug("failed to allocateWindowsResources")
			return nil, r, err
		}
		log.G(ctx).Debug("hcsshim::CreateContainer creating container document")
		v1, v2, err := createWindowsContainerDocument(ctx, coi)
		if err != nil {
			log.G(ctx).WithError(err).Debug("failed createHCSContainerDocument")
			return nil, r, err
		}

		if schemaversion.IsV10(coi.actualSchemaVersion) {
			// v1 Argon or Xenon. Pass the document directly to HCS.
			hcsDocument = v1
		} else if coi.HostingSystem != nil {
			// v2 Xenon. Pass the container object to the UVM.
			gcsDocument = &hcsschema.HostedSystem{
				SchemaVersion: schemaversion.SchemaV21(),
				Container:     v2,
			}
		} else {
			// v2 Argon. Pass the container object to the HCS.
			hcsDocument = &hcsschema.ComputeSystem{
				Owner:                             coi.actualOwner,
				SchemaVersion:                     schemaversion.SchemaV21(),
				ShouldTerminateOnLastHandleClosed: true,
				Container:                         v2,
			}
		}
	}

	log.G(ctx).Debug("hcsshim::CreateContainer creating compute system")
	if gcsDocument != nil {
		c, err := coi.HostingSystem.CreateContainer(ctx, coi.actualID, gcsDocument)
		if err != nil {
			return nil, r, err
		}
		return c, r, nil
	}

	system, err := hcs.CreateComputeSystem(ctx, coi.actualID, hcsDocument)
	if err != nil {
		return nil, r, err
	}

	// if container is process isolated, check if affinityCPUs has been set in createOptions.
	// If yes, set information on the job object created for this container

	if coi.HostingSystem == nil && coi.Spec.Windows != nil {
		err = setCPUAffinityOnJobObject(ctx, coi.Spec, system.ID())
		if err != nil {
			return nil, r, err
		}
	}

	return system, r, nil
}

func setCPUAffinityOnJobObject(ctx context.Context, spec *specs.Spec, computeSystemId string) error {
	//
	if spec == nil || spec.Windows == nil || spec.Windows.Resources == nil ||
		spec.Windows.Resources.CPU == nil || spec.Windows.Resources.CPU.AffinityCPUs == nil {
		return nil
	}

	defaultJobObjectName := fmt.Sprintf(`\Container_%s`, computeSystemId)
	fmt.Printf("default JO name %v", defaultJobObjectName)
	/*
		unicodeJobName, err := winapi.NewUnicodeString(defaultJobObjectName)
		if err != nil {
			return fmt.Errorf("Error getting unicodeJobName %v", err)
		}

			jobHandle, err := winapi.OpenJobObject(winapi.JOB_OBJECT_ALL_ACCESS, 0, unicodeJobName.Buffer)
			if err != nil {
				return fmt.Errorf("Error opening job object %v", err)
			}
	*/
	jobOptions := &jobobject.Options{
		UseNTVariant: true,
		Name:         defaultJobObjectName,
	}
	job, err := jobobject.Open(ctx, jobOptions)
	if err != nil {
		return err
	}
	defer job.Close()
	/*
		// check for numa node number
		if spec.Windows.Resources.CPU.AffinityPreferredNumaNodes != nil {
			// numberOfPreferredNumaNodes := uint32(len(spec.Windows.Resources.CPU.AffinityPreferredNumaNodes))
			// list of numa nodes might need to be set for this container. Therefore ensure we have enough space
			// in attrList for that.
			attrList, err := windows.NewProcThreadAttributeList(2)
			if err != nil {
				return fmt.Errorf("failed to initialize process thread attribute list: %w", err)
			}
			defer attrList.Delete()

			// Set up the process to only inherit stdio handles and nothing else.
			numaNode := (uint16)(spec.Windows.Resources.CPU.AffinityPreferredNumaNodes[0])
			err = attrList.Update(
				windows.PROC_THREAD_ATTRIBUTE_PREFERRED_NODE,
				unsafe.Pointer(&numaNode),
				//uintptr(len(spec.Windows.Resources.CPU.AffinityPreferredNumaNodes))*unsafe.Sizeof(spec.Windows.Resources.CPU.AffinityPreferredNumaNodes[0]),
				//uintptr(unsafe.Sizeof(spec.Windows.Resources.CPU.AffinityPreferredNumaNodes[0])),
				uintptr(unsafe.Sizeof(numaNode)),
			)
			if err != nil {
				return fmt.Errorf("Error updating proc thread attribute for numa node, %v", err)
			}

			err = job.UpdateProcThreadAttribute(attrList)
			if err != nil {
				return fmt.Errorf("Error updating proc thread attribute for JO, %v", err)
			}
		}
	*/
	//	numaNodeInfo := []winapi.JOBOBJECT_CPU_GROUP_AFFINITY{}
	info := []winapi.JOBOBJECT_CPU_GROUP_AFFINITY{}
	if spec.Windows.Resources.CPU.AffinityCPUs != nil {
		info = make([]winapi.JOBOBJECT_CPU_GROUP_AFFINITY, len(spec.Windows.Resources.CPU.AffinityCPUs))
		for i, cpu := range spec.Windows.Resources.CPU.AffinityCPUs {
			info[i].CpuMask = (uintptr)(cpu.CPUMask)
			info[i].CpuGroup = (uint16)(cpu.CPUGroup)
			info[i].Reserved = [3]uint16{0, 0, 0}
		}
	}

	if spec.Windows.Resources.CPU.AffinityPreferredNumaNodes != nil {
		numaNodeInfo, err := coreinfo.GetNumaNodeToProcessorInfo()
		if err != nil {
			return fmt.Errorf("error getting numa node info: %v", err)
		}
		// check if cpu affinities are also set and consolidate the masks
		if len(info) > 0 {
			for _, numaNode := range numaNodeInfo {
				doesCpuAffinityExist := false
				for ind, _ := range info {
					if info[ind].CpuGroup == numaNode.CpuGroup {
						doesCpuAffinityExist = true
						// overwrite the entire mask of the numa node
						info[ind].CpuMask = numaNode.CpuMask
						break
					}
				}
				if !doesCpuAffinityExist {
					info = append(info, numaNode)
				}
			}
		}
	}

	return job.SetInformationJobObject(info)
}

// isV2Xenon returns true if the create options are for a HCS schema V2 xenon container
// with a hosting VM
func (coi *createOptionsInternal) isV2Xenon() bool {
	return schemaversion.IsV21(coi.actualSchemaVersion) && coi.HostingSystem != nil
}

// isV1Xenon returns true if the create options are for a HCS schema V1 xenon container
// with a hosting VM
func (coi *createOptionsInternal) isV1Xenon() bool {
	return schemaversion.IsV10(coi.actualSchemaVersion) && coi.HostingSystem != nil
}

// isV2Argon returns true if the create options are for a HCS schema V2 argon container
// which should have no hosting VM
func (coi *createOptionsInternal) isV2Argon() bool {
	return schemaversion.IsV21(coi.actualSchemaVersion) && coi.HostingSystem == nil
}

// isV1Argon returns true if the create options are for a HCS schema V1 argon container
// which should have no hyperv settings
func (coi *createOptionsInternal) isV1Argon() bool {
	return schemaversion.IsV10(coi.actualSchemaVersion) && coi.Spec.Windows.HyperV == nil
}

func (coi *createOptionsInternal) hasWindowsAssignedDevices() bool {
	return (coi.Spec.Windows != nil) && (coi.Spec.Windows.Devices != nil) &&
		(len(coi.Spec.Windows.Devices) > 0)
}
