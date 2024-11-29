package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
)

// 2E83DEBE-805B-5649-8DE8-5CE2F5B8055B,
var vmid = guid.GUID{
	Data1: 0x2E83DEBE,
	Data2: 0x805B,
	Data3: 0x5649,
	Data4: [8]uint8{0x8D, 0xE8, 0x5C, 0xE2, 0xF5, 0xB8, 0x05, 0x5B},
}

var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

func handleRequest(conn net.Conn) {
	fmt.Printf("Sending reply \n")
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
				fmt.Printf("error sending reply %s", err)
			}
		}

		buffer := make([]byte, 1024)

		// use bufio.Scanner
		length, err := conn.Read(buffer)
		if err != nil {
			//log.Panicln(err)
			errString := fmt.Sprintf("%s", err)
			if !strings.Contains(errString, "EOF") {
				fmt.Printf("error reading %s", err)
			} else {
				continue
			}
		}

		strResp := string(buffer[:length])
		fmt.Printf("Received response on server side:  %d\t:%s\n", length, strResp)

	}
}

func main() {
	f, err := os.OpenFile("C:\\test-gcs-server2-hcsshim.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	hvsockAddr := &winio.HvsockAddr{
		VMID:      vmid,
		ServiceID: WindowsSidecarGcsHvsockServiceID,
	}

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
			fmt.Printf("Err accepting connection %v", err)
		}
		log.Printf("got a new connection con: %v", conn)
		go handleRequest(conn)
	}

}
