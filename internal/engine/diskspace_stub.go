//go:build !windows

package engine

import "fmt"

// FreeBytesOnVolume is a stub for non-Windows builds.
func FreeBytesOnVolume(path string) (int64, error) {
	return 0, fmt.Errorf("disk space query not supported on this platform")
}
