//go:build windows

package engine

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

// FreeBytesOnVolume returns the number of bytes available to the calling
// process on the volume containing path. The path itself does not need to
// exist — only its parent must be reachable. Errors are returned with the
// resolved path embedded for easier debugging.
func FreeBytesOnVolume(path string) (int64, error) {
	if path == "" {
		return 0, fmt.Errorf("empty path")
	}

	// GetDiskFreeSpaceExW wants a directory that exists; walk up if needed.
	probe := filepath.Clean(path)
	for {
		if _, err := syscall.UTF16PtrFromString(probe); err != nil {
			return 0, fmt.Errorf("encode path %q: %w", probe, err)
		}
		// Try the path; if it doesn't exist GetDiskFreeSpaceExW returns
		// ERROR_PATH_NOT_FOUND, in which case we step one directory up and
		// retry. We stop at the volume root.
		free, err := getDiskFreeSpace(probe)
		if err == nil {
			return free, nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return 0, fmt.Errorf("query free space on %q: %w", path, err)
		}
		probe = parent
	}
}

func getDiskFreeSpace(path string) (int64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	ret, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return 0, callErr
	}
	return int64(freeBytesAvailable), nil
}
