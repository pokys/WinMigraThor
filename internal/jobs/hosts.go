//go:build windows

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HostsJob backs up and restores the system hosts file.
type HostsJob struct{}

func (j *HostsJob) Name() string        { return "hosts" }
func (j *HostsJob) Description() string { return "Hosts file (custom DNS entries)" }

func hostsFilePath() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
}

func hostsHasCustomEntries(path string) (bool, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, 0
	}
	var count int
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	// Default hosts file has only the localhost entries
	return count > 2, count
}

func (j *HostsJob) Scan(userPath string) (ScanResult, error) {
	path := hostsFilePath()
	info, err := os.Stat(path)
	if err != nil {
		return ScanResult{}, nil
	}

	custom, count := hostsHasCustomEntries(path)
	detail := "default (no custom entries)"
	if custom {
		detail = fmt.Sprintf("%d entries", count)
	}

	var items []ScanItem
	items = append(items, ScanItem{
		Label:     "hosts",
		Details:   detail,
		SizeBytes: info.Size(),
		Selected:  custom, // only pre-select if there are custom entries
	})

	return ScanResult{Items: items, TotalSizeBytes: info.Size()}, nil
}

func (j *HostsJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	src := hostsFilePath()
	if _, err := os.Stat(src); err != nil {
		result.Status = "skipped"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	dstDir := filepath.Join(target, "hosts")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would copy hosts file")
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	data, err := os.ReadFile(src)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "read hosts: "+err.Error())
		return result, err
	}

	dst := filepath.Join(dstDir, "hosts")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "write hosts: "+err.Error())
		return result, err
	}

	result.FilesCount = 1
	result.SizeBytes = int64(len(data))
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = "success"

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *HostsJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	srcFile := filepath.Join(source, "hosts", "hosts")
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no hosts backup found")
		return result, nil
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would restore hosts file")
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	data, err := os.ReadFile(srcFile)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "read backup: "+err.Error())
		return result, err
	}

	dst := hostsFilePath()
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "write hosts: "+err.Error())
		return result, err
	}

	result.FilesCount = 1
	result.SizeBytes = int64(len(data))
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = "success"

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}
