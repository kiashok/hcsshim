//go:build windows

package schema1

// ContainerProperties holds the properties for a container and the processes running in that container
type ContainerProperties struct {
	ID                           string `json:"Id"`
	State                        string
	Name                         string
	SystemType                   string
	RuntimeOSType                string `json:"RuntimeOsType,omitempty"`
	Owner                        string
	SiloGUID                     string                              `json:"SiloGuid,omitempty"`
	RuntimeID                    guid.GUID                           `json:"RuntimeId,omitempty"`
	IsRuntimeTemplate            bool                                `json:",omitempty"`
	RuntimeImagePath             string                              `json:",omitempty"`
	Stopped                      bool                                `json:",omitempty"`
	ExitType                     string                              `json:",omitempty"`
	AreUpdatesPending            bool                                `json:",omitempty"`
	ObRoot                       string                              `json:",omitempty"`
	Statistics                   Statistics                          `json:",omitempty"`
	ProcessList                  []ProcessListItem                   `json:",omitempty"`
	MappedVirtualDiskControllers map[int]MappedVirtualDiskController `json:",omitempty"`
	GuestConnectionInfo          GuestConnectionInfo                 `json:",omitempty"`
}

// MemoryStats holds the memory statistics for a container
type MemoryStats struct {
	UsageCommitBytes            uint64 `json:"MemoryUsageCommitBytes,omitempty"`
	UsageCommitPeakBytes        uint64 `json:"MemoryUsageCommitPeakBytes,omitempty"`
	UsagePrivateWorkingSetBytes uint64 `json:"MemoryUsagePrivateWorkingSetBytes,omitempty"`
}
