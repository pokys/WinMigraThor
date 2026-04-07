//go:build windows

package checks

import (
	"fmt"
	"os"
	"strings"
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

// EnsureAdminRelaunch relaunches the current executable with elevation when needed.
// It returns relaunched=true when a new elevated process was started and the caller should exit.
func EnsureAdminRelaunch(argv []string) (bool, error) {
	if IsAdmin() {
		return false, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return false, err
	}

	verbPtr, _ := windows.UTF16PtrFromString("runas")
	filePtr, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return false, err
	}

	params := buildAdminParams(argv[1:])
	paramsPtr, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return false, err
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecuteW := shell32.NewProc("ShellExecuteW")
	ret, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		0,
		1,
	)
	if ret <= 32 {
		if callErr != syscall.Errno(0) {
			return false, callErr
		}
		return false, fmt.Errorf("ShellExecuteW failed with code %d", ret)
	}
	return true, nil
}

func buildAdminParams(args []string) string {
	escaped := make([]string, 0, len(args))
	for _, arg := range args {
		escaped = append(escaped, syscall.EscapeArg(arg))
	}
	return strings.Join(escaped, " ")
}
