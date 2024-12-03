package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

type handler struct {
	fromsvc chan error
}

// new guid for my service
// ae8da506-a019-4553-a52b-902bc0fa0411
var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

// accepts new connection closes listener
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
	// Prefer context error to VM error to accept error in order to return the
	// most useful error.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, err
}

func handleRequest(conn net.Conn) {
	log.Printf("Sending reply \n")
	for {
		/*
			buffer := make([]byte, 1024)

			// use bufio.Scanner
			length, err := conn.Read(buffer)
			if err != nil {
				//log.Panicln(err)
				errString := fmt.Sprintf("%s", err)
				if !strings.Contains(errString, "EOF") {
					log.Printf("error reading %s", err)
				} else {
					continue
				}
			}

			str := string(buffer[:length])
			log.Printf("Received command %d\t:%s\n", length, str)
		*/

		str := "CreateContainer request"
		//strreply := fmt.Sprintf("I got %s", str)
		_, err := conn.Write([]byte(str + "\n"))
		if err != nil {
			errString := fmt.Sprintf("%s", err)
			if !strings.Contains(errString, "EOF") {
				log.Printf("error sending reply %s", err)
			}
		}

		buffer := make([]byte, 1024)

		// use bufio.Scanner
		length, err := conn.Read(buffer)
		if err != nil {
			//log.Panicln(err)
			errString := fmt.Sprintf("%s", err)
			if !strings.Contains(errString, "EOF") {
				log.Printf("error reading %s", err)
			} else {
				continue
			}
		}

		strResp := string(buffer[:length])
		log.Printf("Received response on server side:  %d\t:%s\n", length, strResp)

	}
}

func startRecvAndSendLoop() {
	log.Printf("Starting startRecvAndSendLoop()\n")
	ctx := context.Background()

	// 1. Connect to client
	hvsockAddr := &winio.HvsockAddr{
		VMID:      gcs.HV_GUID_PARENT,
		ServiceID: gcs.WindowsSidecarGcsHvsockServiceID,
	}

	fmt.Printf("dialing gcs at address %v", hvsockAddr)
	shimConn, err := winio.Dial(ctx, hvsockAddr)
	if err != nil {
		// error dialing the address
		log.Printf("Error dialing gcs at address %v", hvsockAddr)
		return
	}

	// 2.
	listener, err := winio.ListenHvsock(&winio.HvsockAddr{
		VMID: gcs.HV_GUID_LOOPBACK,
		//HV_GUID_SILOHOST,
		//HV_GUID_PARENT,
		//HV_GUID_LOOPBACK,
		ServiceID: gcs.WindowsGcsHvsockServiceID,
	})
	if err != nil {
		log.Printf("Error to start server for sidecar <-> inbox gcs communication")
		return
	}

	var gcsListener net.Listener
	gcsListener = listener

	// accept connection
	//conn, err = listener.Accept()
	log.Printf("Waiting for service connection from inbox GCS \n")
	var gcsCon net.Conn
	gcsCon, err = acceptAndClose(ctx, gcsListener)
	//serverCon, err = sidecarGcsListener.Accept()
	if err != nil {
		log.Printf("Err accepting connection %v", err)
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go recvFromHcsshimAndSendResponse(shimConn, gcsCon)

	go recvFromGcsAndSendResponse(gcsCon, shimConn)

	wg.Wait()
	///
}

func recvFromHcsshimAndSendResponse(hcsshimCon *winio.HvsockConn, gcsCon net.Conn) {
	fmt.Printf("Receive loop from hcsshim \n")
	buffer := make([]byte, 1024)

	for {
		length, err := hcsshimCon.Read(buffer)
		if err != nil {
			fmt.Printf("Error reading from inbox gcs with err %v", err)
			return
		}

		str := string(buffer[:length])
		fmt.Printf("shimResponse: Received command %d\t:%s\n", length, str)

		/*
			if strings.HasPrefix(str, "CreateContainer") {
				_, err := hvsockCon.Write([]byte(fmt.Sprintf("!! ACK CreateContainer request at time %v \n", time.Now())))
				if err != nil {
					log.Printf("!! Error writing CreateContainer response from sidecar GCS with error %v", err)
					return
				}
			} else if strings.HasPrefix(str, "MountVolume") {
				_, err := hvsockCon.Write([]byte(fmt.Sprintf("!! ACK MountVolume request at time %v \n", time.Now())))
				if err != nil {
					log.Printf("!! Error writing MountVolume response from sidecar GCS with error %v", err)
					return
				}
			}
		*/

		// Forward message got from hcsshim
		_, err = gcsCon.Write([]byte(str))
		if err != nil {
			fmt.Printf("!! Error replying from gcs to sidecar with error %v", err)
			return
		}
	}
}

func recvFromGcsAndSendResponse(gcsCon /*readfrom */ net.Conn, hcsshimCon *winio.HvsockConn) {
	fmt.Printf("Receive loop from inbox gcs \n")
	buffer := make([]byte, 1024)

	for {
		length, err := gcsCon.Read(buffer)
		if err != nil {
			fmt.Printf("Error reading from inbox gcs with err %v", err)
			return
		}

		str := string(buffer[:length])
		fmt.Printf("InboxGCS response: Received command %d\t:%s\n", length, str)

		/*
			if strings.HasPrefix(str, "CreateContainer") {
				_, err := hvsockCon.Write([]byte(fmt.Sprintf("!! ACK CreateContainer request at time %v \n", time.Now())))
				if err != nil {
					log.Printf("!! Error writing CreateContainer response from sidecar GCS with error %v", err)
					return
				}
			} else if strings.HasPrefix(str, "MountVolume") {
				_, err := hvsockCon.Write([]byte(fmt.Sprintf("!! ACK MountVolume request at time %v \n", time.Now())))
				if err != nil {
					log.Printf("!! Error writing MountVolume response from sidecar GCS with error %v", err)
					return
				}
			}
		*/

		_, err = hcsshimCon.Write([]byte(str))
		if err != nil {
			fmt.Printf("!! Error replying from gcs to sidecar with error %v", err)
			return
		}
	}
}

func (m *handler) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	log.Printf("got execute request \n ")
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPauseAndContinue

	status <- svc.Status{State: svc.StartPending}
	//m.hvsockAddrAndDialer.startRecvAndSendLoop()
	//	tick := time.Tick(5 * time.Second)
	m.fromsvc <- nil
	status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

loop:
	for {
		select {
		//	case <-tick:
		//		log.Print("Tick Handled...!")
		//		log.Printf("counter is %v", (m.counter))
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Print("Shutting service...!")
				break loop
			case svc.Pause:
				status <- svc.Status{State: svc.Paused, Accepts: cmdsAccepted}
			case svc.Continue:
				status <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
			default:
				log.Printf("Unexpected service control request #%d", c)
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
				log.Fatalf("Error running service in debug mode.Err: %v", err)
			}
		} else {
			err := svc.Run(name, h)
			if err != nil {
				log.Fatalf("Error running service in Service Control mode.Err %v", err)
			}
		}
		h.fromsvc <- err
	}()

	// Wait for the first signal from the service handler.
	log.Printf("waiting for first signal from scm \n")
	err = <-h.fromsvc
	if err != nil {
		return err
	}
	return nil

}

func main() {
	f, err := os.OpenFile("C:\\service-debug-gcs-sidecar-test1.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	type srvResp struct {
		err error
	}

	chsrv := make(chan error)
	go func() {
		defer close(chsrv)

		if err := runService("gcs-sidecar", false); err != nil {
			log.Fatal(err)
			//?? chsr
		}

		chsrv <- err
	}()

	select {
	//case <-ctx.Done():
	//	return ctx.Err()
	case r := <-chsrv:
		if r != nil {
			log.Fatal(r)

		}
		//srvDetails = r.srvDetails
	}

	startRecvAndSendLoop()
}
