//go:build windows
// +build windows

package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
	shimlog "github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"

	gcsBridge "github.com/Microsoft/hcsshim/cmd/gcs-sidecar/internal/bridge"
)

var (
	logFile  = "C:\\logs\\gcs-sidecar.log"
	logLevel = logrus.TraceLevel
)

type handler struct {
	fromsvc chan error
}

// New guid for sidecar gcs service
// ae8da506-a019-4553-a52b-902bc0fa0411
var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

// Accepts new connection closes listener
func acceptAndClose(ctx context.Context, l net.Listener) (net.Conn, error) {
	var conn net.Conn
	ch := make(chan error)
	go func() {
		var err error
		conn, err = l.Accept()
		ch <- err
	}()
	select {
	case err := <-ch:
		l.Close()
		return conn, err
	case <-ctx.Done():
	}

	l.Close()

	err := <-ch
	if err == nil {
		return conn, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, err
}

func (h *handler) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.Accepted(windows.SERVICE_ACCEPT_PARAMCHANGE)

	status <- svc.Status{State: svc.StartPending, Accepts: 0}
	// unblock runService()
	h.fromsvc <- nil

	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logrus.Println("Shutting service...!")
				break loop
			case svc.Pause:
				status <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				logrus.Printf("Unexpected service control request #%d", c)
			}
		}
	}

	status <- svc.Status{State: svc.StopPending}
	return false, 1
}

func runService(name string, isDebug bool) error {
	h := &handler{
		fromsvc: make(chan error),
	}

	var err error
	go func() {
		if isDebug {
			err := debug.Run(name, h)
			if err != nil {
				logrus.Fatalf("Error running service in debug mode.Err: %v", err)
			}
		} else {
			err := svc.Run(name, h)
			if err != nil {
				logrus.Fatalf("Error running service in Service Control mode.Err %v", err)
			}
		}
		h.fromsvc <- err
	}()

	// Wait for the first signal from the service handler.
	logrus.Tracef("waiting for first signal from service handler\n")
	err = <-h.fromsvc
	if err != nil {
		return err
	}
	return nil

}

func main() {
	ctx := context.Background()

	logFileHandle, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_SYNC|os.O_TRUNC, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
	}
	defer logFileHandle.Close()

	logrus.AddHook(shimlog.NewHook())
	logrus.SetLevel(logLevel)
	logrus.SetOutput(logFileHandle)

	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	trace.RegisterExporter(&oc.LogrusExporter{})

	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(logFileHandle.Fd())); err != nil {
		logrus.WithError(err).Error("error redirecting handle")
		return
	}
	os.Stderr = logFileHandle

	chsrv := make(chan error)
	go func() {
		defer close(chsrv)

		if err := runService("gcs-sidecar", false); err != nil {
			logrus.Fatalf("error starting gcs-sidecar service: %v", err)
		}

		chsrv <- err
	}()

	select {
	case <-ctx.Done():
		logrus.Fatalln("context deadline exceeded")
		return
	case r := <-chsrv:
		if r != nil {
			logrus.Fatal(r)
			return
		}
	}

	// 1. Start external server to connect with inbox GCS
	listener, err := winio.ListenHvsock(&winio.HvsockAddr{
		VMID:      gcs.HV_GUID_LOOPBACK,
		ServiceID: gcs.WindowsGcsHvsockServiceID,
	})
	if err != nil {
		logrus.WithError(err).Errorf("error to start server for sidecar <-> inbox gcs communication")
		return
	}

	var gcsListener net.Listener
	gcsListener = listener
	gcsCon, err := acceptAndClose(ctx, gcsListener)
	if err != nil {
		logrus.WithError(err).Errorf("error accepting inbox GCS connection")
		return
	}

	// 2. Setup connection with hcsshim external gcs connection
	//	var establishedShimConnection chan bool
	hvsockAddr := &winio.HvsockAddr{
		VMID:      gcs.HV_GUID_PARENT,
		ServiceID: gcs.WindowsSidecarGcsHvsockServiceID,
	}

	logrus.WithFields(logrus.Fields{
		"hvsockAddr": hvsockAddr,
	}).Tracef("Dialing to hcsshim external bridge at address %v", hvsockAddr)
	shimCon, err := winio.Dial(ctx, hvsockAddr)
	if err != nil {
		logrus.WithError(err).Errorf("error dialing hcsshim external bridge")
		return
	}

	// TODO:
	// Since gcs-sidecar can be used for all types of hyperv wcow
	// containers. Do we want to have an initial policy enforcer state?
	/*
		var initialEnforcer windowssecuritypolicy.SecurityPolicyEnforcer
		initialPolicyStance := "allow"
		switch initialPolicyStance {
		case "allow":
			initialEnforcer = &windowssecuritypolicy.OpenDoorSecurityPolicyEnforcer{}
			log.Printf("initial-policy-stance: allow")
		case "deny":
			initialEnforcer = &windowssecuritypolicy.ClosedDoorSecurityPolicyEnforcer{}
			log.Printf("initial-policy-stance: deny")
		default:
			log.Printf("unknown initial-policy-stance")
		}
	*/

	// 3. Create bridge and initializa
	brdg := gcsBridge.NewBridge(shimCon, gcsCon, nil)
	brdg.AssignHandlers()

	// 3. Listen and serve for hcsshim requests.
	err = brdg.ListenAndServeShimRequests()
	if err != nil {
		logrus.WithError(err).Errorf("failed to serve request")
	}
}
