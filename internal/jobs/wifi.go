//go:build windows

package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WiFiJob handles WiFi profile backup/restore via netsh.
type WiFiJob struct{}

func (j *WiFiJob) Name() string        { return "wifi" }
func (j *WiFiJob) Description() string { return "WiFi profiles (saved networks + passwords)" }

func (j *WiFiJob) Scan(userPath string) (ScanResult, error) {
	// Get list of WiFi profiles from netsh
	out, err := exec.Command("netsh.exe", "wlan", "show", "profiles").Output()
	if err != nil {
		return ScanResult{}, fmt.Errorf("netsh show profiles: %w", err)
	}

	var items []ScanItem
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				if name != "" && !strings.HasPrefix(name, "---") {
					items = append(items, ScanItem{
						Label:    name,
						Details:  "WiFi network",
						Selected: true,
					})
				}
			}
		}
	}

	return ScanResult{Items: items, TotalSizeBytes: int64(len(items) * 2048)}, nil
}

func (j *WiFiJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	wifiDst := filepath.Join(target, "wifi")
	if err := os.MkdirAll(wifiDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would export WiFi profiles via netsh")
		result.Status = "success"
		return result, nil
	}

	// Export all profiles with keys (passwords) in cleartext XML
	cmd := exec.Command("netsh.exe", "wlan", "export", "profile",
		"key=clear",
		"folder="+wifiDst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, fmt.Sprintf("netsh export: %v\n%s", err, string(out)))
		return result, err
	}

	// Count exported XML files
	entries, _ := os.ReadDir(wifiDst)
	count := 0
	var totalSize int64
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".xml") {
			count++
			info, err := e.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
	}

	result.FilesCount = count
	result.SizeBytes = totalSize
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = "success"

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *WiFiJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	wifiSrc := filepath.Join(source, "wifi")
	if _, err := os.Stat(wifiSrc); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no wifi backup found")
		return result, nil
	}

	entries, err := os.ReadDir(wifiSrc)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	var imported int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
			continue
		}

		if opts.DryRun {
			result.Warnings = append(result.Warnings, "[dry-run] would import: "+e.Name())
			continue
		}

		xmlPath := filepath.Join(wifiSrc, e.Name())
		cmd := exec.Command("netsh.exe", "wlan", "add", "profile",
			"filename="+xmlPath,
			"user=all")
		out, err := cmd.CombinedOutput()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("import %s: %v (%s)", e.Name(), err, strings.TrimSpace(string(out))))
		} else {
			imported++
		}
	}

	result.FilesCount = imported
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}
