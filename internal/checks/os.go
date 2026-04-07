//go:build windows

package checks

import (
	"fmt"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

const (
	minMajor = 10
	minMinor = 0
	minBuild = 19044
)

// OSVersion holds the parsed Windows version numbers.
type OSVersion struct {
	Major int
	Minor int
	Build int
}

func (v OSVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Build)
}

// GetOSVersion reads the Windows version from registry.
func GetOSVersion() (OSVersion, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`,
		registry.QUERY_VALUE)
	if err != nil {
		return OSVersion{}, fmt.Errorf("cannot read Windows version from registry: %w", err)
	}
	defer k.Close()

	major, _, err := k.GetIntegerValue("CurrentMajorVersionNumber")
	if err != nil {
		return OSVersion{}, fmt.Errorf("cannot read CurrentMajorVersionNumber: %w", err)
	}
	minor, _, err := k.GetIntegerValue("CurrentMinorVersionNumber")
	if err != nil {
		return OSVersion{}, fmt.Errorf("cannot read CurrentMinorVersionNumber: %w", err)
	}
	buildStr, _, err := k.GetStringValue("CurrentBuildNumber")
	if err != nil {
		return OSVersion{}, fmt.Errorf("cannot read CurrentBuildNumber: %w", err)
	}
	build, err := strconv.Atoi(buildStr)
	if err != nil {
		return OSVersion{}, fmt.Errorf("cannot parse build number %q: %w", buildStr, err)
	}
	return OSVersion{
		Major: int(major),
		Minor: int(minor),
		Build: build,
	}, nil
}

// CheckOSVersion returns an error if the OS is below the minimum requirement.
func CheckOSVersion() error {
	v, err := GetOSVersion()
	if err != nil {
		return err
	}
	if v.Major < minMajor ||
		(v.Major == minMajor && v.Minor < minMinor) ||
		(v.Major == minMajor && v.Minor == minMinor && v.Build < minBuild) {
		return fmt.Errorf("Windows 10 version 21H2 (build 19044) or later required.\nCurrent version: %s", v)
	}
	return nil
}
