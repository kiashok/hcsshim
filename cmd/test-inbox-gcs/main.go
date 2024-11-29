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
)

var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

// fdb52da4-d1ce-4706-b990-bc20fe25b793
var TestWindowsGcsHvsockServiceID = guid.GUID{
	Data1: 0xfdb52da4,
	Data2: 0xd1ce,
	Data3: 0x4706,
	Data4: [8]uint8{0xb9, 0x90, 0xbc, 0x20, 0xfe, 0x25, 0xb7, 0x93},
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

var WindowsGcsHvsockServiceID = guid.GUID{
	Data1: 0xacef5661,
	Data2: 0x84a1,
	Data3: 0x4e44,
	Data4: [8]uint8{0x85, 0x6b, 0x62, 0x45, 0xe6, 0x9f, 0x46, 0x20},
}

// e0e16197-dd56-4a10-9195-5ee7a155a838
var HV_GUID_LOOPBACK = guid.GUID{
	Data1: 0xe0e16197,
	Data2: 0xdd56,
	Data3: 0x4a10,
	Data4: [8]uint8{0x91, 0x95, 0x5e, 0xe7, 0xa1, 0x55, 0xa8, 0x38},
}

// a42e7cda-d03f-480c-9cc2-a4de20abb878
var HV_GUID_PARENT = guid.GUID{
	Data1: 0xa42e7cda,
	Data2: 0xd03f,
	Data3: 0x480c,
	Data4: [8]uint8{0x9c, 0xc2, 0xa4, 0xde, 0x20, 0xab, 0xb8, 0x78},
}

func main() {
	f, err := os.OpenFile("C:\\test-inbox-gcs.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	// 1. Connect to client
	hvsockAddr := &winio.HvsockAddr{
		VMID:      HV_GUID_LOOPBACK,
		ServiceID: TestWindowsGcsHvsockServiceID,
	}

	ctx := context.Background()
	fmt.Printf("dialing gcs at address %v", hvsockAddr)
	shimConn, err := winio.Dial(ctx, hvsockAddr)
	if err != nil {
		// error dialing the address
		fmt.Printf("Error dialing gcs at address %v with err %v", hvsockAddr, err)
		log.Printf("Error dialing gcs at address %v", hvsockAddr)
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go recvFromSidecarAndSendResponse(shimConn)

	wg.Wait()
	///
	/*
		listener, err := winio.ListenHvsock(hvsockAddr)
		if err != nil {
			//return err
			fmt.Printf("!! err listening to sock add with err %v", err)
			return
		}

		fmt.Printf("! Listeing to server at %v", hvsockAddr)

		log.Printf("! Listeing to server at %v", hvsockAddr)
		var sidecarGcsListener net.Listener
		sidecarGcsListener = listener

		var conn net.Conn

		for {
			//conn, err = listener.Accept()
			conn, err = sidecarGcsListener.Accept()
			if err != nil {
				log.Printf("Err accepting connection %v", err)
			}
			log.Printf("got a new connection con: %v", conn)
			go handleRequest(conn)
		}
	*/
}

func recvFromSidecarAndSendResponse(gcsConn *winio.HvsockConn) {
	fmt.Printf("Receive loop \n")
	buffer := make([]byte, 1024)

	for {
		length, err := gcsConn.Read(buffer)
		if err != nil {
			fmt.Printf("Error reading from inbox gcs with err %v", err)
			return
		}

		str := string(buffer[:length])
		log.Printf("sidecar: Received command %s", str)
		replyString := fmt.Sprintf("sidecar: Received command %s", str)
		fmt.Printf("sidecarResponse: Received command %d\t:%s\n", length, str)

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

		_, err = gcsConn.Write([]byte(replyString))
		if err != nil {
			fmt.Printf("!! Error replying from gcs to sidecar with error %v", err)
			return
		}
	}
}
