//go:build windows

package users

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Profile represents a local Windows user profile.
type Profile struct {
	Username  string
	Path      string
	SizeBytes int64
	IsCurrent bool
}

// SkipUsers contains system/service account names to skip.
var SkipUsers = map[string]bool{
	"Public":             true,
	"Default":            true,
	"Default User":       true,
	"All Users":          true,
	"defaultuser0":       true,
	"WDAGUtilityAccount": true,
	"NetworkService":     true,
	"LocalService":       true,
}

// Detect scans for local user profiles.
func Detect() ([]Profile, error) {
	// Get current user from environment
	currentUser := os.Getenv("USERNAME")

	// Try to get profile paths from registry first
	profilePaths := registryProfiles()

	// Fallback: scan C:\Users
	if len(profilePaths) == 0 {
		profilePaths = scanUsersDir()
	}

	var profiles []Profile
	for username, path := range profilePaths {
		if SkipUsers[username] {
			continue
		}
		if strings.HasPrefix(username, ".") {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		size := dirSize(path)
		profiles = append(profiles, Profile{
			Username:  username,
			Path:      path,
			SizeBytes: size,
			IsCurrent: strings.EqualFold(username, currentUser),
		})
	}
	return profiles, nil
}

func registryProfiles() map[string]string {
	result := make(map[string]string)
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`,
		registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return result
	}
	defer k.Close()

	sids, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return result
	}
	for _, sid := range sids {
		sk, err := registry.OpenKey(k, sid, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		profilePath, _, err := sk.GetStringValue("ProfileImagePath")
		sk.Close()
		if err != nil {
			continue
		}
		// Expand %SystemDrive% and other environment variables.
		profilePath = os.ExpandEnv(profilePath)
		username := filepath.Base(profilePath)
		result[username] = profilePath
	}
	return result
}

func scanUsersDir() map[string]string {
	result := make(map[string]string)
	root := `C:\Users`
	entries, err := os.ReadDir(root)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() {
			name := e.Name()
			result[name] = filepath.Join(root, name)
		}
	}
	return result
}

func dirSize(path string) int64 {
	var size int64
	// Use a quick estimate: just stat top-level dirs
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			// Recursively sum (with a depth limit for speed)
			size += subDirSize(filepath.Join(path, e.Name()), 3)
		} else {
			info, err := e.Info()
			if err == nil {
				size += info.Size()
			}
		}
	}
	return size
}

func subDirSize(path string, depth int) int64 {
	if depth == 0 {
		return 0
	}
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
			size += subDirSize(filepath.Join(path, e.Name()), depth-1)
		} else {
			info, err := e.Info()
			if err == nil {
				size += info.Size()
			}
		}
	}
	return size
}
