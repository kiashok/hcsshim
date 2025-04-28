//go:build windows
// +build windows

package pspdriver

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"

	winio "github.com/Microsoft/go-winio"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName            = "AmdSnpPsp"
	snpFirmwareEnvVariable = "SnpGuestReport"
	privilegeName          = "SeSystemEnvironmentPrivilege"
	amdSevSnpGUIDStr       = "{4c3bddb9-c2b1-4cbd-9e0c-cb45e9e0e168}"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetFirmwareVar = kernel32.NewProc("GetFirmwareEnvironmentVariableW")
)

func StartPSPDriver(ctx context.Context) error {
	// Connect to the Service Control Manager
	m, err := mgr.Connect()
	if err != nil {
		return errors.Wrap(err, "Failed to connect to service manager")
	}
	defer m.Disconnect()

	// Open the service
	s, err := m.OpenService(serviceName)
	if err != nil {
		return errors.Wrapf(err, "Could not access service %q", serviceName)
	}
	defer s.Close()

	// Start the service
	err = s.Start()
	if err != nil {
		return errors.Wrapf(err, "Could not start service %q", serviceName)
	}

	log.G(ctx).Tracef("Service %q started successfully", serviceName)

	// TODO cleanup (kiashok): confirm the running state of the pspdriver
	status, err := s.Query()
	if err != nil {
		return errors.Wrap(err, "could not query service status")
	}

	switch status.State {
	case svc.Running:
		fmt.Println("Service is running.")
	case svc.Stopped:
		fmt.Println("Service is stopped.")
	case svc.StartPending:
		fmt.Println("Service is starting.")
	case svc.StopPending:
		fmt.Println("Service is stopping.")
	default:
		fmt.Printf("Service state: %v\n", status.State)
	}
	return nil
}

// IsSNPEnabled() returns true if SNP support is available.
func IsSNPEnabled(ctx context.Context) bool {
	// GetFirmwareEnvironmentVariableW() requires privelege of SeSystemEnvironmentName.
	// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-getfirmwareenvironmentvariable
	err := winio.EnableProcessPrivileges([]string{privilegeName})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("enabling privilege failed")
		return false
	}

	// UEFI variable name for SNP
	firmwareEnvVar, _ := syscall.UTF16PtrFromString(snpFirmwareEnvVariable)
	amdSnpGUID, _ := syscall.UTF16PtrFromString(amdSevSnpGUIDStr)
	// Prepare buffer for data
	// SNP report is max of 4KB
	buffer := make([]byte, 4096)

	r1, _, err := procGetFirmwareVar.Call(
		uintptr(unsafe.Pointer(firmwareEnvVar)),
		uintptr(unsafe.Pointer(amdSnpGUID)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)

	if r1 == 0 {
		log.G(ctx).WithError(err).Debugf("SNP report not available")
		return false
	}

	return true
}
