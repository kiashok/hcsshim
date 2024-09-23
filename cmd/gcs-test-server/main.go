package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
)

var WindowsSidecarGcsHvsockServiceID = guid.GUID{
	Data1: 0xae8da506,
	Data2: 0xa019,
	Data3: 0x4553,
	Data4: [8]uint8{0xa5, 0x2b, 0x90, 0x2b, 0xc0, 0xfa, 0x04, 0x11},
}

func handleRequest(conn net.Conn) {
	for {
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

		strreply := fmt.Sprintf("I got %s", str)
		conn.Write([]byte(strreply + "\n"))

	}
}

func main() {
	f, err := os.OpenFile("C:/service-debug-gcs-sidecar-server.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln(fmt.Errorf("error opening file: %v", err))
	}
	defer f.Close()

	log.SetOutput(f)

	hvsockAddr := &winio.HvsockAddr{
		VMID:      gcs.HV_GUID_LOOPBACK,
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
			log.Printf("Err accepting connection %v", err)
		}
		log.Printf("got a new connection con: %v", conn)
		go handleRequest(conn)
	}

}
