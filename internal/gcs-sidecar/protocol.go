//go:build windows
// +build windows

<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
package bridge
========
package prot
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Microsoft/go-winio/pkg/guid"
<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
========
	"github.com/Microsoft/hcsshim/internal/bridgeutils/commonutils"
	"github.com/Microsoft/hcsshim/internal/hcs/schema1"
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
)

const (
	hdrSize    = 16
	hdrOffType = 0
	hdrOffSize = 4
	hdrOffID   = 8

	// maxMsgSize is the maximum size of an incoming message. This is not
	// enforced by the guest today but some maximum must be set to avoid
	// unbounded allocations.
	maxMsgSize = 0x10000

	msgTypeRequest  msgType = 0x10100000
	msgTypeResponse msgType = 0x20100000
	msgTypeNotify   msgType = 0x30100000
	msgTypeMask     msgType = 0xfff00000

	notifyContainer = 1<<8 | 1
)

// SequenceID is used to correlate requests and responses.
type SequenceID uint64

// MessageHeader is the common header present in all communications messages.
type MessageHeader struct {
	Type msgType
	Size uint32
	ID   SequenceID
}

type msgType uint32

<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
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
========
type AnyInString struct {
	Value interface{}
}

func (a *AnyInString) MarshalText() ([]byte, error) {
	return json.Marshal(a.Value)
}

func (a *AnyInString) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, &a.Value)
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
}

type RpcProc uint32

const (
	RpcCreate RpcProc = (iota+1)<<8 | 1
	RpcStart
	RpcShutdownGraceful
	RpcShutdownForced
	RpcExecuteProcess
	RpcWaitForProcess
	RpcSignalProcess
	RpcResizeConsole
	RpcGetProperties
	RpcModifySettings
	RpcNegotiateProtocol
	RpcDumpStacks
	RpcDeleteContainerState
	RpcUpdateContainer
	RpcLifecycleNotification
)

func (rpc RpcProc) String() string {
	switch rpc {
	case RpcCreate:
		return "Create"
	case RpcStart:
		return "Start"
	case RpcShutdownGraceful:
		return "ShutdownGraceful"
	case RpcShutdownForced:
		return "ShutdownForced"
	case RpcExecuteProcess:
		return "ExecuteProcess"
	case RpcWaitForProcess:
		return "WaitForProcess"
	case RpcSignalProcess:
		return "SignalProcess"
	case RpcResizeConsole:
		return "ResizeConsole"
	case RpcGetProperties:
		return "GetProperties"
	case RpcModifySettings:
		return "ModifySettings"
	case RpcNegotiateProtocol:
		return "NegotiateProtocol"
	case RpcDumpStacks:
		return "DumpStacks"
	case RpcDeleteContainerState:
		return "DeleteContainerState"
	case RpcUpdateContainer:
		return "UpdateContainer"
	case RpcLifecycleNotification:
		return "LifecycleNotification"
	default:
		return "0x" + strconv.FormatUint(uint64(rpc), 16)
	}
}

<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
type anyInString struct {
	Value interface{}
}

func (a *anyInString) MarshalText() ([]byte, error) {
	return json.Marshal(a.Value)
}

func (a *anyInString) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, &a.Value)
========
type MsgType uint32

const (
	MsgTypeRequest  MsgType = 0x10100000
	MsgTypeResponse MsgType = 0x20100000
	MsgTypeNotify   MsgType = 0x30100000
	MsgTypeMask     MsgType = 0xfff00000

	NotifyContainer = 1<<8 | 1
)

func (typ MsgType) String() string {
	var s string
	switch typ & MsgTypeMask {
	case MsgTypeRequest:
		s = "Request("
	case MsgTypeResponse:
		s = "Response("
	case MsgTypeNotify:
		s = "Notify("
		switch typ - MsgTypeNotify {
		case NotifyContainer:
			s += "Container"
		default:
			s += fmt.Sprintf("%#x", uint32(typ))
		}
		return s + ")"
	default:
		return fmt.Sprintf("%#x", uint32(typ))
	}
	s += RpcProc(typ &^ MsgTypeMask).String()
	return s + ")"
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
}

// Ocspancontext is the internal JSON representation of the OpenCensus
// `trace.SpanContext` for fowarding to a GCS that supports it.
type Ocspancontext struct {
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

type RequestBase struct {
	ContainerID string    `json:"ContainerId"`
	ActivityID  guid.GUID `json:"ActivityId"`

	// OpenCensusSpanContext is the encoded OpenCensus `trace.SpanContext` if
	// set when making the request.
	//
	// NOTE: This is not a part of the protocol but because its a JSON protocol
	// adding fields is a non-breaking change. If the guest supports it this is
	// just additive context.
	OpenCensusSpanContext *Ocspancontext `json:"ocsc,omitempty"`
}

<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
type responseBase struct {
	Result       int32         // HResult
	ErrorMessage string        `json:",omitempty"`
	ActivityID   guid.GUID     `json:"ActivityId,omitempty"`
	ErrorRecords []errorRecord `json:",omitempty"`
}

func (req *requestBase) Base() *requestBase {
	return req
}

func (resp *responseBase) Base() *responseBase {
	return resp
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

type negotiateProtocolRequest struct {
	requestBase
========
func (req *RequestBase) Base() *RequestBase {
	return req
}

type ResponseBase struct {
	Result       int32                     // HResult
	ErrorMessage string                    `json:",omitempty"`
	ActivityID   guid.GUID                 `json:"ActivityId,omitempty"`
	ErrorRecords []commonutils.ErrorRecord `json:",omitempty"`
}

func (resp *ResponseBase) Base() *ResponseBase {
	return resp
}

type NegotiateProtocolRequest struct {
	RequestBase
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
	MinimumVersion uint32
	MaximumVersion uint32
}

<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
type hcsschemaVersion struct {
	Major int32 `json:"Major,omitempty"`

	Minor int32 `json:"Minor,omitempty"`
}

type gcsCapabilities struct {
	SendHostCreateMessage      bool
	SendHostStartMessage       bool
	HvSocketConfigOnStartup    bool
	SendLifecycleNotifications bool
	SupportedSchemaVersions    []hcsschemaVersion
	RuntimeOsType              string
	GuestDefinedCapabilities   interface{}
}

type negotiateProtocolResponse struct {
	responseBase
========
type NegotiateProtocolResponse struct {
	ResponseBase
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
	Version      uint32          `json:",omitempty"`
	Capabilities GcsCapabilities `json:",omitempty"`
}

type DumpStacksRequest struct {
	RequestBase
}

type DumpStacksResponse struct {
	ResponseBase
	GuestStacks string
}

type DeleteContainerStateRequest struct {
	RequestBase
}

type ContainerCreate struct {
	RequestBase
	ContainerConfig AnyInString
}

type UvmConfig struct {
	SystemType          string // must be "Container"
	TimeZoneInformation *hcsschema.TimeZoneInformation
}

type ContainerNotification struct {
	RequestBase
	Type       string      // Compute.System.NotificationType
	Operation  string      // Compute.System.ActiveOperation
	Result     int32       // HResult
	ResultInfo AnyInString `json:",omitempty"`
}

type ContainerExecuteProcess struct {
	RequestBase
	Settings ExecuteProcessSettings
}

type ExecuteProcessSettings struct {
	ProcessParameters       AnyInString
	StdioRelaySettings      *ExecuteProcessStdioRelaySettings      `json:",omitempty"`
	VsockStdioRelaySettings *ExecuteProcessVsockStdioRelaySettings `json:",omitempty"`
}

type ExecuteProcessStdioRelaySettings struct {
	StdIn  *guid.GUID `json:",omitempty"`
	StdOut *guid.GUID `json:",omitempty"`
	StdErr *guid.GUID `json:",omitempty"`
}

type ExecuteProcessVsockStdioRelaySettings struct {
	StdIn  uint32 `json:",omitempty"`
	StdOut uint32 `json:",omitempty"`
	StdErr uint32 `json:",omitempty"`
}

type ContainerResizeConsole struct {
	RequestBase
	ProcessID uint32 `json:"ProcessId"`
	Height    uint16
	Width     uint16
}

<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
type containerModifySettings struct {
	requestBase
	Request interface{}
}

type containerWaitForProcess struct {
	requestBase
========
type ContainerWaitForProcess struct {
	RequestBase
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
	ProcessID   uint32 `json:"ProcessId"`
	TimeoutInMs uint32
}

type ContainerSignalProcess struct {
	RequestBase
	ProcessID uint32      `json:"ProcessId"`
	Options   interface{} `json:",omitempty"`
}
<<<<<<<< HEAD:internal/gcs-sidecar/protocol.go
========

type ContainerPropertiesQuery schema1.PropertyQuery

func (q *ContainerPropertiesQuery) MarshalText() ([]byte, error) {
	return json.Marshal((*schema1.PropertyQuery)(q))
}

func (q *ContainerPropertiesQuery) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*schema1.PropertyQuery)(q))
}

type ContainerPropertiesQueryV2 hcsschema.PropertyQuery

func (q *ContainerPropertiesQueryV2) MarshalText() ([]byte, error) {
	return json.Marshal((*hcsschema.PropertyQuery)(q))
}

func (q *ContainerPropertiesQueryV2) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*hcsschema.PropertyQuery)(q))
}

type ContainerGetProperties struct {
	RequestBase
	Query ContainerPropertiesQuery
}

type ContainerGetPropertiesV2 struct {
	RequestBase
	Query ContainerPropertiesQueryV2
}

type ContainerModifySettings struct {
	RequestBase
	Request interface{}
}

type GcsCapabilities struct {
	SendHostCreateMessage      bool
	SendHostStartMessage       bool
	HvSocketConfigOnStartup    bool
	SendLifecycleNotifications bool
	SupportedSchemaVersions    []hcsschema.Version
	RuntimeOsType              string
	GuestDefinedCapabilities   json.RawMessage
}

type ContainerCreateResponse struct {
	ResponseBase
}

type ContainerExecuteProcessResponse struct {
	ResponseBase
	ProcessID uint32 `json:"ProcessId"`
}

type ContainerWaitForProcessResponse struct {
	ResponseBase
	ExitCode uint32
}

type ContainerProperties schema1.ContainerProperties

func (p *ContainerProperties) MarshalText() ([]byte, error) {
	return json.Marshal((*schema1.ContainerProperties)(p))
}

func (p *ContainerProperties) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*schema1.ContainerProperties)(p))
}

type ContainerPropertiesV2 hcsschema.Properties

func (p *ContainerPropertiesV2) MarshalText() ([]byte, error) {
	return json.Marshal((*hcsschema.Properties)(p))
}

func (p *ContainerPropertiesV2) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, (*hcsschema.Properties)(p))
}

type ContainerGetPropertiesResponse struct {
	ResponseBase
	Properties ContainerProperties
}

type ContainerGetPropertiesResponseV2 struct {
	ResponseBase
	Properties ContainerPropertiesV2
}
>>>>>>>> 92b788140 (Refactor common bridge protocol code for reuse):internal/gcs/prot/protocol.go
