//go:build windows
// +build windows

package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
	"go.opencensus.io/trace/tracestate"
	"golang.org/x/sys/windows"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/bridgeutils/commonutils"
	"github.com/Microsoft/hcsshim/internal/bridgeutils/gcserr"
	prot "github.com/Microsoft/hcsshim/internal/gcs"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/Microsoft/hcsshim/pkg/securitypolicy"
)

type Bridge struct {
	mu        sync.Mutex
	hostState *Host
	// List of handlers for handling different rpc message requests.
	rpcHandlerList map[prot.RpcProc]HandlerFunc

	// hcsshim and inbox GCS connections respectively.
	shimConn     io.ReadWriteCloser
	inboxGCSConn io.ReadWriteCloser

	// Response channels to forward incoming requests to inbox GCS
	// and send responses back to hcsshim respectively.
	sendToGCSCh  chan request
	sendToShimCh chan bridgeResponse
}

// SequenceID is used to correlate requests and responses.
type sequenceID uint64

// messageHeader is the common header present in all communications messages.
type messageHeader struct {
	Type prot.MsgType
	Size uint32
	ID   sequenceID
}

type bridgeResponse struct {
	ctx      context.Context
	header   messageHeader
	response []byte
}

type request struct {
	// Context created once received from the bridge.
	ctx context.Context
	// header is the wire format message header that preceded the message for
	// this request.
	header messageHeader
	// activityID is the id of the specific activity for this request.
	activityID guid.GUID
	// message is the portion of the request that follows the `Header`.
	message []byte
}

func NewBridge(shimConn io.ReadWriteCloser, inboxGCSConn io.ReadWriteCloser, initialEnforcer securitypolicy.SecurityPolicyEnforcer) *Bridge {
	hostState := NewHost(initialEnforcer)
	return &Bridge{
		rpcHandlerList: make(map[prot.RpcProc]HandlerFunc),
		hostState:      hostState,
		shimConn:       shimConn,
		inboxGCSConn:   inboxGCSConn,
		sendToGCSCh:    make(chan request),
		sendToShimCh:   make(chan bridgeResponse),
	}
}

func NewPolicyEnforcer(initialEnforcer securitypolicy.SecurityPolicyEnforcer) *SecurityPoliyEnforcer {
	return &SecurityPoliyEnforcer{
		securityPolicyEnforcerSet: false,
		securityPolicyEnforcer:    initialEnforcer,
	}
}

// UnknownMessage represents the default handler logic for an unmatched request
// type sent from the bridge.
func UnknownMessage(r *request) error {
	log.G(r.ctx).Debugf("bridge: function not supported, header type %v", prot.MsgType(r.header.Type).String())
	return gcserr.WrapHresult(errors.Errorf("bridge: function not supported, header type: %v", r.header.Type), gcserr.HrNotImpl)
}

// HandlerFunc is an adapter to use functions as handlers.
type HandlerFunc func(*request) error

// ServeMsg serves request by calling appropriate handler functions.
func (b *Bridge) ServeMsg(r *request) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if r == nil {
		panic("bridge: nil request to handler")
	}

	var handler HandlerFunc
	var ok bool
	messageType := r.header.Type
	rpcProcID := prot.RpcProc(prot.MsgType(messageType) &^ prot.MsgTypeMask)
	if handler, ok = b.rpcHandlerList[rpcProcID]; !ok {
		return UnknownMessage(r)
	}

	return handler(r)
}

// Handle registers the handler for the given message id and protocol version.
func (b *Bridge) Handle(rpcProcID prot.RpcProc, handlerFunc HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if handlerFunc == nil {
		panic("empty function handler")
	}

	if _, ok := b.rpcHandlerList[rpcProcID]; ok {
		logrus.WithFields(logrus.Fields{
			"message-type": rpcProcID.String(),
		}).Warn("overwriting bridge handler")
	}

	b.rpcHandlerList[rpcProcID] = handlerFunc
}

func (b *Bridge) HandleFunc(rpcProcID prot.RpcProc, handler func(*request) error) {
	if handler == nil {
		panic("bridge: nil handler func")
	}

	b.Handle(rpcProcID, HandlerFunc(handler))
}

// AssignHandlers creates and assigns appropriate event handlers
// for the different bridge message types.
func (b *Bridge) AssignHandlers() {
	b.HandleFunc(prot.RpcCreate, b.createContainer)
	b.HandleFunc(prot.RpcStart, b.startContainer)
	b.HandleFunc(prot.RpcShutdownGraceful, b.shutdownGraceful)
	b.HandleFunc(prot.RpcShutdownForced, b.shutdownForced)
	b.HandleFunc(prot.RpcExecuteProcess, b.executeProcess)
	b.HandleFunc(prot.RpcWaitForProcess, b.waitForProcess)
	b.HandleFunc(prot.RpcSignalProcess, b.signalProcess)
	b.HandleFunc(prot.RpcResizeConsole, b.resizeConsole)
	b.HandleFunc(prot.RpcGetProperties, b.getProperties)
	b.HandleFunc(prot.RpcModifySettings, b.modifySettings)
	b.HandleFunc(prot.RpcNegotiateProtocol, b.negotiateProtocol)
	b.HandleFunc(prot.RpcDumpStacks, b.dumpStacks)
	b.HandleFunc(prot.RpcDeleteContainerState, b.deleteContainerState)
	b.HandleFunc(prot.RpcUpdateContainer, b.updateContainer)
	b.HandleFunc(prot.RpcLifecycleNotification, b.lifecycleNotification)
}

// readMessage reads the message from io.Reader
func readMessage(r io.Reader) (messageHeader, []byte, error) {
	var h [prot.HdrSize]byte
	_, err := io.ReadFull(r, h[:])
	if err != nil {
		return messageHeader{}, nil, err
	}
	var header messageHeader
	buf := bytes.NewReader(h[:])
	err = binary.Read(buf, binary.LittleEndian, &header)
	if err != nil {
		logrus.WithError(err).Errorf("error reading message header")
		return messageHeader{}, nil, err
	}

	n := header.Size
	if n < prot.HdrSize || n > prot.MaxMsgSize {
		logrus.Errorf("invalid message size %d", n)
		return messageHeader{}, nil, fmt.Errorf("invalid message size %d", n)
	}

	n -= prot.HdrSize
	msg := make([]byte, n)
	_, err = io.ReadFull(r, msg)
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return messageHeader{}, nil, err
	}

	return header, msg, nil
}

func isLocalDisconnectError(err error) bool {
	return errors.Is(err, windows.WSAECONNABORTED)
}

// Sends request to the inbox GCS channel
func (b *Bridge) forwardRequestToGcs(req *request) {
	b.sendToGCSCh <- *req
}

// Sends response to the hcsshim channel
func (b *Bridge) sendResponseToShim(ctx context.Context, rpcProcType prot.RpcProc, id sequenceID, response interface{}) error {
	// TODO (kiashok):
	respType := prot.MsgTypeResponse | prot.MsgType(rpcProcType)
	msgb, err := json.Marshal(response)
	if err != nil {
		return err
	}
	msgHeader := messageHeader{
		Type: respType,
		Size: uint32(len(msgb) + prot.HdrSize),
		ID:   id,
	}

	b.sendToShimCh <- bridgeResponse{
		ctx:      ctx,
		header:   msgHeader,
		response: msgb,
	}
	return nil
}

func getContextAndSpan(baseSpanCtx *prot.Ocspancontext) (context.Context, *trace.Span) {
	var ctx context.Context
	var span *trace.Span
	if baseSpanCtx != nil {
		sc := trace.SpanContext{}
		if bytes, err := hex.DecodeString(baseSpanCtx.TraceID); err == nil {
			copy(sc.TraceID[:], bytes)
		}
		if bytes, err := hex.DecodeString(baseSpanCtx.SpanID); err == nil {
			copy(sc.SpanID[:], bytes)
		}
		sc.TraceOptions = trace.TraceOptions(baseSpanCtx.TraceOptions)
		if baseSpanCtx.Tracestate != "" {
			if bytes, err := base64.StdEncoding.DecodeString(baseSpanCtx.Tracestate); err == nil {
				var entries []tracestate.Entry
				if err := json.Unmarshal(bytes, &entries); err == nil {
					if ts, err := tracestate.New(nil, entries...); err == nil {
						sc.Tracestate = ts
					}
				}
			}
		}
		ctx, span = oc.StartSpanWithRemoteParent(
			context.Background(),
			"sidecar::request",
			sc,
			oc.WithServerSpanKind,
		)
	} else {
		ctx, span = oc.StartSpan(
			context.Background(),
			"sidecar::request",
			oc.WithServerSpanKind,
		)
	}

	return ctx, span
}

// ListenAndServeShimRequests listens to messages on the hcsshim
// and inbox GCS connections and schedules them for processing.
// After processing, messages are forwarded to inbox GCS on success
// and responses from inbox GCS or error messages are sent back
// to hcsshim via bridge connection.
func (b *Bridge) ListenAndServeShimRequests() error {
	shimRequestChan := make(chan request)
	sideErrChan := make(chan error)

	defer b.inboxGCSConn.Close()
	defer close(shimRequestChan)
	defer close(sideErrChan)
	defer b.shimConn.Close()
	defer close(b.sendToShimCh)
	defer close(b.sendToGCSCh)

	// Listen to requests from hcsshim
	go func() {
		var recverr error
		br := bufio.NewReader(b.shimConn)
		for {
			header, msg, err := readMessage(br)
			if err != nil {
				if err == io.EOF || isLocalDisconnectError(err) {
					return
				}
				recverr = errors.Wrap(err, "bridge read from shim connection failed")
				logrus.Error(recverr)
				break
			}
			var msgBase prot.RequestBase
			_ = json.Unmarshal(msg, &msgBase)
			ctx, span := getContextAndSpan(msgBase.OpenCensusSpanContext)
			span.AddAttributes(
				trace.Int64Attribute("message-id", int64(header.ID)),
				trace.StringAttribute("message-type", header.Type.String()),
				trace.StringAttribute("activityID", msgBase.ActivityID.String()),
				trace.StringAttribute("containerID", msgBase.ContainerID))

			req := request{
				ctx:        ctx,
				activityID: msgBase.ActivityID,
				header:     header,
				message:    msg,
			}
			shimRequestChan <- req
		}
		sideErrChan <- recverr
	}()
	// Process each bridge request received from shim asynchronously.
	go func() {
		for req := range shimRequestChan {
			go func(req request) {
				if err := b.ServeMsg(&req); err != nil {
					log.G(req.ctx).WithError(err).Errorf("failed to serve request: %v", req.header.Type.String())
					// In case of error, create appropriate response message to
					// be sent to hcsshim.
					resp := &prot.ResponseBase{
						Result:       int32(windows.ERROR_GEN_FAILURE),
						ErrorMessage: err.Error(),
						ActivityID:   req.activityID,
					}
					setErrorForResponseBase(resp, err, "gcs-sidecar" /* moduleName */)
					b.sendResponseToShim(req.ctx, prot.RpcProc(prot.MsgTypeResponse), req.header.ID, resp)
				}
			}(req)
		}
	}()
	go func() {
		var err error
		for req := range b.sendToGCSCh {
			// Forward message to gcs
			log.G(req.ctx).Tracef("bridge send to gcs, req %v", req)
			buffer, err := b.prepareResponseMessage(req.header, req.message)
			if err != nil {
				err = errors.Wrap(err, "error preparing response")
				logrus.Error(err)
				break
			}

			_, err = buffer.WriteTo(b.inboxGCSConn)
			if err != nil {
				err = errors.Wrap(err, "err forwarding shim req to inbox GCS")
				logrus.Error(err)
				break
			}
		}
		sideErrChan <- err
	}()
	// Receive response from gcs and forward to hcsshim
	go func() {
		var recverr error
		for {
			header, message, err := readMessage(b.inboxGCSConn)
			if err != nil {
				if err == io.EOF || isLocalDisconnectError(err) {
					return
				}
				recverr = errors.Wrap(err, "bridge read from gcs failed")
				logrus.Error(recverr)
				break
			}

			// Forward to shim
			resp := bridgeResponse{
				ctx:      context.Background(), //TODO
				header:   header,
				response: message,
			}
			b.sendToShimCh <- resp
		}
		sideErrChan <- recverr
	}()
	// Send response to hcsshim
	go func() {
		var sendErr error
		for resp := range b.sendToShimCh {
			// Send response to shim
			logrus.Tracef("Send response to shim. Header:{ID: %v, Type: %v, Size: %v} msg: %v", resp.header.ID,
				resp.header.Type, resp.header.Size, string(resp.response))
			buffer, err := b.prepareResponseMessage(resp.header, resp.response)
			if err != nil {
				sendErr = errors.Wrap(err, "error preparing response")
				logrus.Error(sendErr)
				break
			}
			_, sendErr = buffer.WriteTo(b.shimConn)
			if sendErr != nil {
				sendErr = errors.Wrap(sendErr, "err sending response to shim")
				logrus.Error(sendErr)
				break
			}
		}
		sideErrChan <- sendErr
	}()

	select {
	case err := <-sideErrChan:
		return err
	}
}

// Prepare response message
func (b *Bridge) prepareResponseMessage(header messageHeader, message []byte) (bytes.Buffer, error) {
	// Create a buffer to hold the serialized header data
	var headerBuf bytes.Buffer
	err := binary.Write(&headerBuf, binary.LittleEndian, header)
	if err != nil {
		return headerBuf, err
	}

	// Write message header followed by actual payload.
	var buf bytes.Buffer
	buf.Write(headerBuf.Bytes())
	buf.Write(message[:])
	return buf, nil
}

// setErrorForResponseBase modifies the passed-in ResponseBase to
// contain information pertaining to the given error.
func setErrorForResponseBase(response *prot.ResponseBase, errForResponse error, moduleName string) {
	hresult, errorMessage, newRecord := commonutils.SetErrorForResponseBaseUtil(errForResponse, moduleName)
	response.Result = int32(hresult)
	response.ErrorMessage = errorMessage
	response.ErrorRecords = append(response.ErrorRecords, newRecord)
}
