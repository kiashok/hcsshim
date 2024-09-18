package hcs

import (
	"github.com/Microsoft/hcsshim/hcs/internal/schema1"
	hcsschema "github.com/Microsoft/hcsshim/hcs/internal/schema2"
)

// ContainerProperties holds the properties for a container and the processes running in that container
type ContainerProperties = schema1.ContainerProperties

// MemoryStats holds the memory statistics for a container
type MemoryStats = schema1.MemoryStats

// ProcessorStats holds the processor statistics for a container
type ProcessorStats = schema1.ProcessorStats

// StorageStats holds the storage statistics for a container
type StorageStats = schema1.StorageStats

// NetworkStats holds the network statistics for a container
type NetworkStats = schema1.NetworkStats

// Statistics is the structure returned by a statistics call on a container
type Statistics = schema1.Statistics

// ProcessList is the structure of an item returned by a ProcessList call on a container
type ProcessListItem = schema1.ProcessListItem

// MappedVirtualDiskController is the structure of an item returned by a MappedVirtualDiskList call on a container
type MappedVirtualDiskController = schema1.MappedVirtualDiskController

// Type of Request Support in ModifySystem
type RequestType = schema1.RequestType

// Type of Resource Support in ModifySystem
type ResourceType = schema1.ResourceType

// RequestType const
const (
	Add     = schema1.Add
	Remove  = schema1.Remove
	Network = schema1.Network
)

// ResourceModificationRequestResponse is the structure used to send request to the container to modify the system
// Supported resource types are Network and Request Types are Add/Remove
type ResourceModificationRequestResponse = schema1.ResourceModificationRequestResponse

type Version = hcsschema.Version

// ProcessConfig is used as both the input of Container.CreateProcess
// and to convert the parameters to JSON for passing onto the HCS
type ProcessConfig = schema1.ProcessConfig

type Layer = schema1.Layer
type MappedDir = schema1.MappedDir
type MappedPipe = schema1.MappedPipe
type HvRuntime = schema1.HvRuntime
type MappedVirtualDisk = schema1.MappedVirtualDisk

// AssignedDevice represents a device that has been directly assigned to a container
//
// NOTE: Support added in RS5
type AssignedDevice = schema1.AssignedDevice

// ContainerConfig is used as both the input of CreateContainer
// and to convert the parameters to JSON for passing onto the HCS
type ContainerConfig = schema1.ContainerConfig

type ComputeSystemQuery = schema1.ComputeSystemQuery

type PropertyType = schema1.PropertyType

type PropertyQuery = schema1.PropertyQuery

type GuestDefinedCapabilities = schema1.GuestDefinedCapabilities 

const (
	PropertyTypeStatistics        PropertyType = "Statistics"        // V1 and V2
	PropertyTypeProcessList       PropertyType = "ProcessList"       // V1 and V2
	PropertyTypeMappedVirtualDisk PropertyType = "MappedVirtualDisk" // Not supported in V2 schema call
	PropertyTypeGuestConnection   PropertyType = "GuestConnection"   // V1 and V2. Nil return from HCS before RS5
)
