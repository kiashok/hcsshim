package resourcepaths

const (
	GPUResourcePath                  string = "VirtualMachine/ComputeTopology/Gpu"
	MemoryResourcePath               string = "VirtualMachine/ComputeTopology/Memory/SizeInMB"
	CPUGroupResourcePath             string = "VirtualMachine/ComputeTopology/Processor/CpuGroup"
	IdledResourcePath                string = "VirtualMachine/ComputeTopology/Processor/IdledProcessors"
	CPUFrequencyPowerCapResourcePath string = "VirtualMachine/ComputeTopology/Processor/CpuFrequencyPowerCap"
	CPULimitsResourcePath            string = "VirtualMachine/ComputeTopology/Processor/Limits"
	SerialResourceFormat             string = "VirtualMachine/Devices/ComPorts/%d"
	FlexibleIovResourceFormat        string = "VirtualMachine/Devices/FlexibleIov/%s"
	LicensingResourcePath            string = "VirtualMachine/Devices/Licensing"
	MappedPipeResourcePrefix         string = "VirtualMachine/Devices/MappedPipes/"
	MappedPipeResourceFormat         string = "VirtualMachine/Devices/MappedPipes/%s"
	NetworkResourcePrefix            string = "VirtualMachine/Devices/NetworkAdapters/"
	NetworkResourceFormat            string = "VirtualMachine/Devices/NetworkAdapters/%s"
	Plan9ShareResourcePath           string = "VirtualMachine/Devices/Plan9/Shares"
	SCSIResourcePrefix               string = "VirtualMachine/Devices/Scsi/"
	SCSIResourceFormat               string = "VirtualMachine/Devices/Scsi/%s/Attachments/%d"
	SharedMemoryRegionResourcePath   string = "VirtualMachine/Devices/SharedMemory/Regions"
	VirtualPCIResourcePrefix         string = "VirtualMachine/Devices/VirtualPci/"
	VirtualPCIResourceFormat         string = "VirtualMachine/Devices/VirtualPci/%s"
	VPMemControllerResourceFormat    string = "VirtualMachine/Devices/VirtualPMem/Devices/%d"
	VPMemDeviceResourceFormat        string = "VirtualMachine/Devices/VirtualPMem/Devices/%d/Mappings/%d"
	VSMBShareResourcePath            string = "VirtualMachine/Devices/VirtualSmb/Shares"
	HvSocketConfigResourcePrefix     string = "VirtualMachine/Devices/HvSocket/HvSocketConfig/ServiceTable/"
	HvSocketConfigResourceFormat     string = "VirtualMachine/Devices/HvSocket/HvSocketConfig/ServiceTable/%s"
)
