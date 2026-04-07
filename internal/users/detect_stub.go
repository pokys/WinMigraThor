//go:build !windows

package users

import (
	"os"
	"os/user"
)

// Detect returns a stub profile based on the current OS user.
// This is used on non-Windows platforms (development/CI).
func Detect() ([]Profile, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	home := u.HomeDir
	if home == "" {
		home = os.Getenv("HOME")
	}
	return []Profile{
		{
			Username:  u.Username,
			Path:      home,
			SizeBytes: 0,
			IsCurrent: true,
		},
	}, nil
}
