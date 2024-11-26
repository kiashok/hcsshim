//go:build windows
// +build windows

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

// new guid for my service
// ae8da506-a019-4553-a52b-902bc0fa0411

type hvSockDetails struct {
	hvsockAddr *winio.HvsockAddr
	//hvsockDialer *winio.HvsockDialer
}

type handler struct {
	hvsockAddrAndDialer *hvSockDetails
	fromsvc             chan error
}

func (hv *hvSockDetails) startRecvAndSendLoop() {
	log.Printf("Starting startRecvAndSendLoop()\n")
	ctx := context.Background()

	// TODO: start the server for GCS bridge client to connect to.
	// 1. use named pipes for communication as its more performant?!
	// POC uses hvsocket with loopback address

	serverListener, err := winio.ListenHvsock(&winio.HvsockAddr{
		VMID:      gcs.HV_GUID_LOOPBACK,
		ServiceID: gcs.WindowsGcsHvsockServiceID,
	})
	if err != nil {
		log.Printf("Error to start server for sidecar <-> inbox gcs communication")
		return
	}

	// accept connection
	//conn, err = listener.Accept()

	var serverCon net.Conn
	serverCon, err = serverListener.Accept()
	if err != nil {
		log.Printf("Err accepting connection %v", err)
		return
	}
	log.Printf("got a new connection con: %v", serverCon)
	//go handleRequest(conn)

	// 2. Establish connection with hcsshim
	hcsshimCon, err := winio.Dial(ctx, hv.hvsockAddr)
	if err != nil {
		// error dialing the address
		log.Printf("Error dialing hvsock sidecar listener at address %v", hv.hvsockAddr)
		return
	}

	//var wg sync.WaitGroup
	//wg.Add(1)
	//	go
	recvLoop(hcsshimCon, serverCon)

	sendLoop(hcsshimCon, serverCon)
	//go sendToHvSocketListener(hvsockCon)

	//wg.Wait()

}

func sendLoop(hcsshimCon *winio.HvsockConn, serverCon net.Conn) {
	log.Printf("Receive loop \n")
	buffer := make([]byte, 1024)

	for {
		length, err := serverCon.Read(buffer)
		if err != nil {
			log.Printf("Error reading from inbox gcs with err %v", err)
			return
		}

		str := string(buffer[:length])
		log.Printf("Received command %d\t:%s\n", length, str)

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
			log.Printf("!! Error forwarding inbox gcs request to hcsshim with error %v", err)
			return
		}
	}
}

func recvLoop(hcsshimCon *winio.HvsockConn, serverCon net.Conn) {
	//, wg *sync.WaitGroup) {
	//defer wg.Done()
	log.Printf("Receive loop \n")
	buffer := make([]byte, 1024)

	for {
		length, err := hcsshimCon.Read(buffer)
		if err != nil {
			log.Printf("Error reading from hvsock with err %v", err)
			return
		}

		str := string(buffer[:length])
		log.Printf("Received command %d\t:%s\n", length, str)

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

		_, err = serverCon.Write([]byte(str))
		if err != nil {
			log.Printf("!! Error forwarding hcsshim request to inbox gcs with error %v", err)
			return
		}
	}
}

func sendToHvSocketListener(hvsockCon *winio.HvsockConn) {
	//defer wg.Done()
	log.Printf("Starting SendLoop() \n")
	// write data
	for {
		_, err := hvsockCon.Write([]byte(fmt.Sprintf("!! Hello from sidecar GCS at time %v \n", time.Now())))
		if err != nil {
			log.Printf("!! Error writing to sidecar GCS with error %v", err)
			return
		}
		// any delay
	}
	return
}

// TODO: code for option 2 - inbox GCS calls into sidecar executable through named pipes OR
// register callbacks and made it call into it?

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

func runService(name string, isDebug bool, hvsockAddr *winio.HvsockAddr, hvsockDialer *winio.HvsockDialer) error {
	h := &handler{
		hvsockAddrAndDialer: &hvSockDetails{
			hvsockAddr: hvsockAddr,
			//	hvsockDialer: hvsockDialer,
		},
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

// [guid]::NewGuid()
// sidecar gcs GUID
// ae8da506-a019-4553-a52b-902bc0fa0411

// Option 1 sidecar before GCS

func main() {
	f, err := os.OpenFile("C:\\service-debug-gcs-sidecar-test1.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	//

	type srvResp struct {
		srvDetails *hvSockDetails
		//		// /err error
	}
	//
	chsrv := make(chan srvResp)
	go func() {
		defer close(chsrv)

		hvsockAddr := &winio.HvsockAddr{
			VMID:      gcs.HV_GUID_PARENT, //gcs.HV_GUID_LOOPBACK, // HV_GUID_PARENT
			ServiceID: WindowsSidecarGcsHvsockServiceID,
		}

		/*
			hcsockDialer := &winio.HvsockDialer{
				Deadline:  time.Now().Add(10 * time.Minute),
				Retries:   1000,
				RetryWait: time.Second,
			}
		*/
		if err := runService("gcs-sidecar", false, hvsockAddr, nil); err != nil {
			log.Fatal(err)
		}

		//hv := &hvSockDetails{hvsockAddr: hvsockAddr, hvsockDialer: hcsockDialer}
		select {
		case chsrv <- srvResp{srvDetails: &hvSockDetails{hvsockAddr: hvsockAddr}}:
		}
	}()

	var srvDetails *hvSockDetails
	select {
	//case <-ctx.Done():
	//	return ctx.Err()
	case r := <-chsrv:
		//	if r.err != nil {
		//		return r.err
		//	}
		srvDetails = r.srvDetails
	}

	srvDetails.startRecvAndSendLoop()

	/*
	   	hvsockAddr := &winio.HvsockAddr{
	   		VMID:      gcs.HV_GUID_LOOPBACK, // HV_GUID_PARENT
	   		ServiceID: WindowsSidecarGcsHvsockServiceID,
	   	}

	   	hcsockDialer := &winio.HvsockDialer{
	   		Deadline:  time.Now().Add(10 * time.Minute),
	   		Retries:   1000,
	   		RetryWait: time.Second,
	   	}

	   hv := hvSockDetails{hvsockAddr: hvsockAddr, hvsockDialer: hcsockDialer}
	   hv.startRecvAndSendLoop()
	*/
}
