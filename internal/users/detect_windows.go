//go:build windows

package users

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// systemSIDPrefixes are SIDs that belong to built-in system accounts — skip them.
// S-1-5-18 = SYSTEM, S-1-5-19 = LOCAL SERVICE, S-1-5-20 = NETWORK SERVICE
var systemSIDPrefixes = []string{
	"S-1-5-18",
	"S-1-5-19",
	"S-1-5-20",
}

// skipByName are profile folder names that are never real user accounts.
var skipByName = map[string]bool{
	"Public":             true,
	"Default":            true,
	"Default User":       true,
	"All Users":          true,
	"defaultuser0":       true,
	"WDAGUtilityAccount": true,
	"NetworkService":     true,
	"LocalService":       true,
	"systemprofile":      true,
}

// Detect scans for local user profiles, including domain accounts.
func Detect() ([]Profile, error) {
	// %USERNAME% is just the short name (without DOMAIN\) — correct for matching
	currentUser := strings.ToLower(os.Getenv("USERNAME"))

	profiles := registryProfiles()

	// Fallback if registry returned nothing
	if len(profiles) == 0 {
		profiles = scanUsersDir()
	}

	var result []Profile
	for _, p := range profiles {
		result = append(result, p)
		// Mark current user — compare short name only (handles DOMAIN\user and user.DOMAIN)
		shortName := strings.ToLower(shortUsername(p.Username))
		p.IsCurrent = shortName == currentUser
		result[len(result)-1] = p
	}
	return result, nil
}

// shortUsername strips domain prefix/suffix from a username.
// "CORP\john.doe" → "john.doe"
// "john.doe@corp.local" → "john.doe"
func shortUsername(name string) string {
	// DOMAIN\user
	if i := strings.LastIndex(name, `\`); i >= 0 {
		return name[i+1:]
	}
	// user@domain
	if i := strings.Index(name, "@"); i >= 0 {
		return name[:i]
	}
	return name
}

func isSystemSID(sid string) bool {
	for _, prefix := range systemSIDPrefixes {
		if strings.HasPrefix(sid, prefix) {
			return true
		}
	}
	return false
}

func registryProfiles() []Profile {
	var profiles []Profile

	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`,
		registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return profiles
	}
	defer k.Close()

	sids, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return profiles
	}

	for _, sid := range sids {
		// Skip known system SIDs immediately
		if isSystemSID(sid) {
			continue
		}

		sk, err := registry.OpenKey(k, sid, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		profilePath, _, err := sk.GetStringValue("ProfileImagePath")
		sk.Close()
		if err != nil {
			continue
		}

		// Expand environment variables like %SystemDrive%
		profilePath = os.ExpandEnv(profilePath)

		// Resolve the actual folder name as the display username
		folderName := filepath.Base(profilePath)

		// Skip by folder name
		if skipByName[folderName] {
			continue
		}
		if strings.HasPrefix(folderName, ".") {
			continue
		}

		// Verify the profile directory actually exists
		info, err := os.Stat(profilePath)
		if err != nil || !info.IsDir() {
			continue
		}

		// Skip profiles outside Users directories (real system profiles live
		// under C:\Windows\system32\config\systemprofile etc.)
		lower := strings.ToLower(profilePath)
		if strings.Contains(lower, `windows\system32`) ||
			strings.Contains(lower, `windows\servicepro`) {
			continue
		}

		size := dirSize(profilePath)
		profiles = append(profiles, Profile{
			Username:  folderName,
			Path:      profilePath,
			SizeBytes: size,
		})
	}
	return profiles
}

func scanUsersDir() []Profile {
	var profiles []Profile
	// Check both C:\Users and %SystemDrive%\Users
	roots := []string{`C:\Users`}
	if sd := os.Getenv("SystemDrive"); sd != "" && !strings.EqualFold(sd, "C:") {
		roots = append(roots, sd+`\Users`)
	}

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if skipByName[name] || strings.HasPrefix(name, ".") {
				continue
			}
			path := filepath.Join(root, name)
			profiles = append(profiles, Profile{
				Username:  name,
				Path:      path,
				SizeBytes: dirSize(path),
			})
		}
	}
	return profiles
}

func dirSize(path string) int64 {
	var size int64
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() {
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
