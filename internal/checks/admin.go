//go:build windows

package checks

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsAdmin returns true if the current process is running with Administrator privileges.
func IsAdmin() bool {
	// Try shell32 IsUserAnAdmin first
	shell32 := syscall.NewLazyDLL("shell32.dll")
	isUserAnAdmin := shell32.NewProc("IsUserAnAdmin")
	ret, _, _ := isUserAnAdmin.Call()
	if ret != 0 {
		return true
	}
	// Fallback: check token elevation
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	// TOKEN_ELEVATION struct: just a DWORD
	type tokenElevation struct {
		TokenIsElevated uint32
	}
	var elevation tokenElevation
	var size uint32
	err = windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &size)
	if err != nil {
		return false
	}
	return elevation.TokenIsElevated != 0
}

// CheckAdmin returns an error if not running as Administrator.
func CheckAdmin() error {
	if !IsAdmin() {
		return fmt.Errorf("migrator must be run as Administrator.\n\nRight-click migrator.exe → \"Run as administrator\"")
	}
	return nil
}
