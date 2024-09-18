//go:build windows
// +build windows

package main

import (
	"context"
	"fmt"
	"os"
	"time"

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

// [guid]::NewGuid()
// sidecar gcs GUID
// ae8da506-a019-4553-a52b-902bc0fa0411

// Option 1 sidecar before GCS
func main() {
	addr := &winio.HvsockAddr{
		VMID:      gcs.HV_GUID_PARENT,
		ServiceID: WindowsSidecarGcsHvsockServiceID,
	}

	d := &winio.HvsockDialer{
		Deadline:  time.Now().Add(10 * time.Minute),
		Retries:   1000,
		RetryWait: time.Second,
	}

	// file to record the logs
	file, err := os.OpenFile("sidecar-gcs-logs.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("Error opening file: %v", err)
		return
	}
	defer file.Close()

	ctx := context.Background()
	hvsockCon, err := d.Dial(ctx, addr)
	if err != nil {
		// error dialing the address
		_, _ = file.WriteString(fmt.Sprintf("Error dialing hvsock sidecar listener at address %v", addr))
		return
	}

	go sendToHvSocketListener(hvsockCon, file)

	// TODO: start the server for GCS bridge client to connect to.
	// 1. use named pipes for communication
	// Another option would be to use hvsocket with loopback address
	/*
		l, err := winio.ListenHvsock(&winio.HvsockAddr{
			VMID:      uvm.runtimeID,
			ServiceID: gcs.WindowsGcsHvsockServiceID,
		})
		if err != nil {
			return err
		}
		uvm.gcListener = l
		return nil
	*/
}

func sendToHvSocketListener(hvsockCon *winio.HvsockConn, file *os.File) {
	// write data
	for {
		_, err := hvsockCon.Write([]byte(fmt.Sprintf("!! From sidecar GCS at time %v", time.Now())))
		if err != nil {
			file.WriteString(fmt.Sprintf("!! Error writing to sidecar GCS at time %v", time.Now()))
			return
		}
		// any delay
	}
	return
}

// TODO: code for option 2 - inbox GCS calls into sidecar executable through named pipes OR
// register callbacks and made it call into it?
