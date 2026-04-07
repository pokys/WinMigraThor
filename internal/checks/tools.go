package checks

import (
	"fmt"
	"os/exec"
)

// ToolCheck holds the result of an external tool check.
type ToolCheck struct {
	Name      string
	Path      string
	Available bool
	Required  bool
	Error     string
}

// CheckTools verifies that required external tools are available.
// Returns a slice of ToolCheck results and a combined error for any required tool that is missing.
func CheckTools() ([]ToolCheck, error) {
	tools := []struct {
		name     string
		binary   string
		required bool
	}{
		{"robocopy", "robocopy.exe", true},
		{"netsh", "netsh.exe", true},
		{"winget", "winget.exe", false},
	}

	var results []ToolCheck
	var missing []string

	for _, t := range tools {
		tc := ToolCheck{
			Name:     t.name,
			Required: t.required,
		}
		path, err := exec.LookPath(t.binary)
		if err == nil {
			tc.Available = true
			tc.Path = path
		} else {
			tc.Error = err.Error()
			if t.required {
				missing = append(missing, t.name)
			}
		}
		results = append(results, tc)
	}

	if len(missing) > 0 {
		return results, fmt.Errorf("required tools not found: %v\n\nEnsure you are running on a supported Windows installation.", missing)
	}
	return results, nil
}

// WingetAvailable returns true if winget is available.
func WingetAvailable() bool {
	_, err := exec.LookPath("winget.exe")
	return err == nil
}
