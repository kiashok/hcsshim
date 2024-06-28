//go:build windows

package coreinfo

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/Microsoft/hcsshim/internal/winapi"
)

// Relationshiptype
type RelationType int

const (
	RelationProcessorCore = iota
	RelationNumaNode
	RelationCache
	RelationProcessorPackage
	RelationGroup
	RelationProcessorDie
	RelationNumaNodeEx
	RelationProcessorModule
	RelationAll = 0xffff
)

type _SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX struct {
	// value is Relationshiptype
	Relationship uint32
	Size         uint32
	/*
		union {
		  PROCESSOR_RELATIONSHIP Processor;
		  NUMA_NODE_RELATIONSHIP NumaNode;
		  CACHE_RELATIONSHIP     Cache;
		  GROUP_RELATIONSHIP     Group;
		} DUMMYUNIONNAME;
	*/
	data interface{}
}

type _NUMA_NODE_RELATIONSHIP struct {
	NodeNumber uint32
	Reserved   [18]byte
	GroupCount uint16
	/*
		union {
		  GROUP_AFFINITY GroupMask;
		  GROUP_AFFINITY GroupMasks[ANYSIZE_ARRAY];
		} DUMMYUNIONNAME;
	*/
	GroupMasks interface{}
}

func GetNumaNodeToProcessorInfo() ([]winapi.JOBOBJECT_CPU_GROUP_AFFINITY, error) {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	getLogicalProcessorInfo := k32.NewProc("GetLogicalProcessorInformationEx")

	// Call once to get the length of data to return
	var returnLength uint32 = 0
	relation := RelationNumaNodeEx

	r1, _, err := getLogicalProcessorInfo.Call(
		uintptr(relation),
		uintptr(0),
		uintptr(unsafe.Pointer(&returnLength)),
	)
	if r1 != 0 && err.(syscall.Errno) != syscall.ERROR_INSUFFICIENT_BUFFER {
		return nil, fmt.Errorf("Call to GetLogicalProcessorInformationEx failed: %v", err)
	}

	// Allocate the buffer with the length it should be
	buffer := make([]byte, returnLength)

	// Call GetLogicalProcessorInformationEx again to get the actual information
	r1, _, err = getLogicalProcessorInfo.Call(
		uintptr(relation),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&returnLength)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("Call to GetLogicalProcessorInformationEx failed: %v", err)
	}

	var groupMasks []winapi.JOBOBJECT_CPU_GROUP_AFFINITY
	for offset := 0; offset < len(buffer); {
		info := (*_SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX)(unsafe.Pointer(&buffer[offset]))
		numaNodeRelationship := (*_NUMA_NODE_RELATIONSHIP)(unsafe.Pointer(&info.data))
		fmt.Printf("Numa Node #%d\n", numaNodeRelationship.NodeNumber)

		groupMasks := make([]winapi.JOBOBJECT_CPU_GROUP_AFFINITY, numaNodeRelationship.GroupCount)
		for i := 0; i < int(numaNodeRelationship.GroupCount); i++ {
			groupMasks[i] = *(*winapi.JOBOBJECT_CPU_GROUP_AFFINITY)(unsafe.Pointer(uintptr(unsafe.Pointer(&numaNodeRelationship.GroupMasks)) + uintptr(i)*unsafe.Sizeof(winapi.JOBOBJECT_CPU_GROUP_AFFINITY{})))
		}

		offset += int(info.Size)
	}

	return groupMasks, nil
}
