//go:build windows

package gcs

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/hcs/schema1"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
)

// LinuxGcsVsockPort is the vsock port number that the Linux GCS will
// connect to.
const LinuxGcsVsockPort = 0x40000000

// WindowsSidecarGcsHvsockServiceID is the hvsock service ID that the Windows GCS
// sidecar will connect to.
var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

// WindowsGcsHvsockServiceID is the hvsock service ID that the Windows GCS
// will connect to.
var WindowsGcsHvsockServiceID = guid.GUID{
	Data1: 0xacef5661,
	Data2: 0x84a1,
	Data3: 0x4e44,
	Data4: [8]uint8{0x85, 0x6b, 0x62, 0x45, 0xe6, 0x9f, 0x46, 0x20},
}

// WindowsGcsHvHostID is the hvsock address for the parent of the VM running the GCS
var WindowsGcsHvHostID = guid.GUID{
	Data1: 0x894cc2d6,
	Data2: 0x9d79,
	Data3: 0x424f,
	Data4: [8]uint8{0x93, 0xfe, 0x42, 0x96, 0x9a, 0xe6, 0xd8, 0xd1},
}

// e0e16197-dd56-4a10-9195-5ee7a155a838
var HV_GUID_LOOPBACK = guid.GUID{
	Data1: 0xe0e16197,
	Data2: 0xdd56,
	Data3: 0x4a10,
	Data4: [8]uint8{0x91, 0x95, 0x5e, 0xe7, 0xa1, 0x55, 0xa8, 0x38},
}

// HV_GUID_SILOHOST:
//
// Address of a silo's host partition.
//
// The silo host of a hosted silo is the utility VM.
// The silo host of a silo on a physical host is the physical host.
//
// 36bd0c5c-7276-4223-88ba-7d03b654c568
var HV_GUID_SILOHOST = guid.GUID{
	Data1: 0x36bd0c5c,
	Data2: 0x7276,
	Data3: 0x4223,
	Data4: [8]uint8{0x88, 0xba, 0x7d, 0x03, 0xb6, 0x54, 0xc5, 0x68},
}

//DEFINE_GUID(HV_GUID_SILOHOST, 0x36bd0c5c, 0x7276, 0x4223, 0x88, 0xba, 0x7d, 0x03, 0xb6, 0x54, 0xc5, 0x68);

// a42e7cda-d03f-480c-9cc2-a4de20abb878
var HV_GUID_PARENT = guid.GUID{
	Data1: 0xa42e7cda,
	Data2: 0xd03f,
	Data3: 0x480c,
	Data4: [8]uint8{0x9c, 0xc2, 0xa4, 0xde, 0x20, 0xab, 0xb8, 0x78},
}

type anyInString struct {
	Value interface{}
}

func (a *anyInString) MarshalText() ([]byte, error) {
	return json.Marshal(a.Value)
}

func (a *anyInString) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, &a.Value)
}

type rpcProc uint32

const (
	rpcCreate rpcProc = (iota+1)<<8 | 1
	rpcStart
	rpcShutdownGraceful
	rpcShutdownForced
	rpcExecuteProcess
	rpcWaitForProcess
	rpcSignalProcess
	rpcResizeConsole
	rpcGetProperties
	rpcModifySettings
	rpcNegotiateProtocol
	rpcDumpStacks
	rpcDeleteContainerState
	rpcUpdateContainer
	rpcLifecycleNotification
)

func (rpc rpcProc) String() string {
	switch rpc {
	case rpcCreate:
		return "Create"
	case rpcStart:
		return "Start"
	case rpcShutdownGraceful:
		return "ShutdownGraceful"
	case rpcShutdownForced:
		return "ShutdownForced"
	case rpcExecuteProcess:
		return "ExecuteProcess"
	case rpcWaitForProcess:
		return "WaitForProcess"
	case rpcSignalProcess:
		return "SignalProcess"
	case rpcResizeConsole:
		return "ResizeConsole"
	case rpcGetProperties:
		return "GetProperties"
	case rpcModifySettings:
		return "ModifySettings"
	case rpcNegotiateProtocol:
		return "NegotiateProtocol"
	case rpcDumpStacks:
		return "DumpStacks"
	case rpcDeleteContainerState:
		return "DeleteContainerState"
	case rpcUpdateContainer:
		return "UpdateContainer"
	case rpcLifecycleNotification:
		return "LifecycleNotification"
	default:
		return "0x" + strconv.FormatUint(uint64(rpc), 16)
	}
}

type msgType uint32

const (
	msgTypeRequest  msgType = 0x10100000
	msgTypeResponse msgType = 0x20100000
	msgTypeNotify   msgType = 0x30100000
	msgTypeMask     msgType = 0xfff00000

	notifyContainer = 1<<8 | 1
)

func (typ msgType) String() string {
	var s string
	switch typ & msgTypeMask {
	case msgTypeRequest:
		s = "Request("
	case msgTypeResponse:
		s = "Response("
	case msgTypeNotify:
		s = "Notify("
		switch typ - msgTypeNotify {
		case notifyContainer:
			s += "Container"
		default:
			s += fmt.Sprintf("%#x", uint32(typ))
		}
		return s + ")"
	default:
		return fmt.Sprintf("%#x", uint32(typ))
	}
	s += rpcProc(typ &^ msgTypeMask).String()
	return s + ")"
}

// ocspancontext is the internal JSON representation of the OpenCensus
// `trace.SpanContext` for fowarding to a GCS that supports it.
type ocspancontext struct {
	// TraceID is the `hex` encoded string of the OpenCensus
	// `SpanContext.TraceID` to propagate to the guest.
	TraceID string `json:",omitempty"`
	// SpanID is the `hex` encoded string of the OpenCensus `SpanContext.SpanID`
	// to propagate to the guest.
	SpanID string `json:",omitempty"`

	// TraceOptions is the OpenCensus `SpanContext.TraceOptions` passed through
	// to propagate to the guest.
	TraceOptions uint32 `json:",omitempty"`

	// Tracestate is the `base64` encoded string of marshaling the OpenCensus
	// `SpanContext.TraceState.Entries()` to JSON.
	//
	// If `SpanContext.Tracestate == nil ||
	// len(SpanContext.Tracestate.Entries()) == 0` this will be `""`.
	Tracestate string `json:",omitempty"`
}

type requestBase struct {
	ContainerID string    `json:"ContainerId"`
	ActivityID  guid.GUID `json:"ActivityId"`

	// OpenCensusSpanContext is the encoded OpenCensus `trace.SpanContext` if
	// set when making the request.
	//
	// NOTE: This is not a part of the protocol but because its a JSON protocol
	// adding fields is a non-breaking change. If the guest supports it this is
	// just additive context.
	OpenCensusSpanContext *ocspancontext `json:"ocsc,omitempty"`
}

func (req *requestBase) Base() *requestBase {
	return req
}

type responseBase struct {
	Result       int32         // HResult
	ErrorMessage string        `json:",omitempty"`
	ActivityID   guid.GUID     `json:"ActivityId,omitempty"`
	ErrorRecords []errorRecord `json:",omitempty"`
}

type errorRecord struct {
	Result       int32 // HResult
	Message      string
	StackTrace   string `json:",omitempty"`
	ModuleName   string
	FileName     string
	Line         uint32
	FunctionName string `json:",omitempty"`
}

func (resp *responseBase) Base() *responseBase {
	return resp
}

type negotiateProtocolRequest struct {
	requestBase
	MinimumVersion uint32
	MaximumVersion uint32
}

type negotiateProtocolResponse struct {
	responseBase
	Version      uint32          `json:",omitempty"`
	Capabilities gcsCapabilities `json:",omitempty"`
}

type dumpStacksRequest struct {
	requestBase
}

type dumpStacksResponse struct {
	responseBase
	GuestStacks string
}

type deleteContainerStateRequest struct {
	requestBase
}

type containerCreate struct {
	requestBase
	ContainerConfig anyInString
}

type uvmConfig struct {
	SystemType          string // must be "Container"
	TimeZoneInformation *hcsschema.TimeZoneInformation
}

type containerNotification struct {
	requestBase
	Type       string      // Compute.System.NotificationType
	Operation  string      // Compute.System.ActiveOperation
	Result     int32       // HResult
	ResultInfo anyInString `json:",omitempty"`
}

type containerExecuteProcess struct {
	requestBase
	Settings executeProcessSettings
}

type executeProcessSettings struct {
	ProcessParameters       anyInString
	StdioRelaySettings      *executeProcessStdioRelaySettings      `json:",omitempty"`
	VsockStdioRelaySettings *executeProcessVsockStdioRelaySettings `json:",omitempty"`
}

type executeProcessStdioRelaySettings struct {
	StdIn  *guid.GUID `json:",omitempty"`
	StdOut *guid.GUID `json:",omitempty"`
	StdErr *guid.GUID `json:",omitempty"`
}

type executeProcessVsockStdioRelaySettings struct {
	StdIn  uint32 `json:",omitempty"`
	StdOut uint32 `json:",omitempty"`
	StdErr uint32 `json:",omitempty"`
}

type containerResizeConsole struct {
	requestBase
	ProcessID uint32 `json:"ProcessId"`
	Height    uint16
	Width     uint16
}

type containerWaitForProcess struct {
	requestBase
	ProcessID   uint32 `json:"ProcessId"`
	TimeoutInMs uint32
}

type containerSignalProcess struct {
	requestBase
	ProcessID uint32      `json:"ProcessId"`
	Options   interface{} `json:",omitempty"`
}

type containerPropertiesQuery schema1.PropertyQuery

func (q *containerPropertiesQuery) MarshalText() ([]byte, error) {
	return json.Marshal((*schema1.PropertyQuery)(q))
}

func (q *containerPropertiesQuery) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*schema1.PropertyQuery)(q))
}

type containerPropertiesQueryV2 hcsschema.PropertyQuery

func (q *containerPropertiesQueryV2) MarshalText() ([]byte, error) {
	return json.Marshal((*hcsschema.PropertyQuery)(q))
}

func (q *containerPropertiesQueryV2) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*hcsschema.PropertyQuery)(q))
}

type containerGetProperties struct {
	requestBase
	Query containerPropertiesQuery
}

type containerGetPropertiesV2 struct {
	requestBase
	Query containerPropertiesQueryV2
}

type containerModifySettings struct {
	requestBase
	Request interface{}
}

type gcsCapabilities struct {
	SendHostCreateMessage      bool
	SendHostStartMessage       bool
	HvSocketConfigOnStartup    bool
	SendLifecycleNotifications bool
	SupportedSchemaVersions    []hcsschema.Version
	RuntimeOsType              string
	GuestDefinedCapabilities   interface{}
}

type containerCreateResponse struct {
	responseBase
}

type containerExecuteProcessResponse struct {
	responseBase
	ProcessID uint32 `json:"ProcessId"`
}

type containerWaitForProcessResponse struct {
	responseBase
	ExitCode uint32
}

type containerProperties schema1.ContainerProperties

func (p *containerProperties) MarshalText() ([]byte, error) {
	return json.Marshal((*schema1.ContainerProperties)(p))
}

func (p *containerProperties) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*schema1.ContainerProperties)(p))
}

type containerPropertiesV2 hcsschema.Properties

func (p *containerPropertiesV2) MarshalText() ([]byte, error) {
	return json.Marshal((*hcsschema.Properties)(p))
}

func (p *containerPropertiesV2) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*hcsschema.Properties)(p))
}

type containerGetPropertiesResponse struct {
	responseBase
	Properties containerProperties
}

type containerGetPropertiesResponseV2 struct {
	responseBase
	Properties containerPropertiesV2
}
