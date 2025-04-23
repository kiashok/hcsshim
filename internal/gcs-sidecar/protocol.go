//go:build windows
// +build windows

package bridge

import "github.com/Microsoft/hcsshim/internal/gcs/prot"

// SequenceID is used to correlate requests and responses.
type sequenceID uint64

// messageHeader is the common header present in all communications messages.
type messageHeader struct {
	Type prot.MsgType
	Size uint32
	ID   sequenceID
}
