//go:build windows

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pokys/winmigrathor/internal/engine"
)

// VPNJob backs up per-user VPN phonebook files (.pbk).
type VPNJob struct{}

func (j *VPNJob) Name() string        { return "vpn" }
func (j *VPNJob) Description() string { return "VPN connections (PBK phonebook files)" }

func (j *VPNJob) Scan(userPath string) (ScanResult, error) {
	files, err := vpnPhonebookFiles(userPath)
	if err != nil {
		return ScanResult{}, err
	}

	var items []ScanItem
	var total int64
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		total += info.Size()
		items = append(items, ScanItem{
			Label:     filepath.Base(file),
			Path:      file,
			SizeBytes: info.Size(),
			Selected:  true,
		})
	}

	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *VPNJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	files, err := vpnPhonebookFiles(userPath)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}
	if len(files) == 0 {
		result.Status = "skipped"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	vpnDst := filepath.Join(target, "vpn")
	if err := os.MkdirAll(vpnDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "vpn-backup.log")
	}

	for _, src := range files {
		if opts.DryRun {
			result.Warnings = append(result.Warnings, "[dry-run] would copy "+src)
			continue
		}

		dstDir := vpnDst
		if err := engine.CopyFile(src, dstDir, logFile); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filepath.Base(src), err))
			continue
		}

		if info, err := os.Stat(src); err == nil {
			result.SizeBytes += info.Size()
		}
		result.FilesCount++
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *VPNJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	vpnSrc := filepath.Join(source, "vpn")
	entries, err := os.ReadDir(vpnSrc)
	if err != nil {
		if os.IsNotExist(err) {
			result.Status = "skipped"
			return result, nil
		}
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	vpnDst := filepath.Join(userPath, "AppData", "Roaming", "Microsoft", "Network", "Connections", "Pbk")
	if err := os.MkdirAll(vpnDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "vpn-restore.log")
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".pbk") {
			continue
		}

		src := filepath.Join(vpnSrc, entry.Name())
		if opts.DryRun {
			result.Warnings = append(result.Warnings, "[dry-run] would restore "+entry.Name())
			continue
		}

		if err := engine.CopyFile(src, vpnDst, logFile); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}

		if info, err := os.Stat(src); err == nil {
			result.SizeBytes += info.Size()
		}
		result.FilesCount++
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func vpnPhonebookFiles(userPath string) ([]string, error) {
	vpnDir := filepath.Join(userPath, "AppData", "Roaming", "Microsoft", "Network", "Connections", "Pbk")
	entries, err := os.ReadDir(vpnDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".pbk") {
			continue
		}
		files = append(files, filepath.Join(vpnDir, entry.Name()))
	}
	return files, nil
}
