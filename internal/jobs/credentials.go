//go:build windows

package jobs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pokys/winmigrathor/internal/engine"
)

// CredentialsJob backs up Windows Credential Manager vaults.
type CredentialsJob struct{}

func (j *CredentialsJob) Name() string        { return "credentials" }
func (j *CredentialsJob) Description() string { return "Windows Credential Manager (vault data)" }

// vaultDirs returns the Credential Manager vault locations for a user.
func vaultDirs(userPath string) []struct {
	Name string
	Path string
} {
	return []struct {
		Name string
		Path string
	}{
		{"Vault (Local)", filepath.Join(userPath, "AppData", "Local", "Microsoft", "Vault")},
		{"Vault (Roaming)", filepath.Join(userPath, "AppData", "Roaming", "Microsoft", "Vault")},
		{"Credentials (Local)", filepath.Join(userPath, "AppData", "Local", "Microsoft", "Credentials")},
		{"Credentials (Roaming)", filepath.Join(userPath, "AppData", "Roaming", "Microsoft", "Credentials")},
		{"Protect (Local)", filepath.Join(userPath, "AppData", "Local", "Microsoft", "Protect")},
		{"Protect (Roaming)", filepath.Join(userPath, "AppData", "Roaming", "Microsoft", "Protect")},
	}
}

func (j *CredentialsJob) Scan(userPath string) (ScanResult, error) {
	var items []ScanItem
	var total int64

	for _, vd := range vaultDirs(userPath) {
		if _, err := os.Stat(vd.Path); os.IsNotExist(err) {
			continue
		}
		size := folderSize(vd.Path)
		total += size
		items = append(items, ScanItem{
			Label:     vd.Name,
			Path:      vd.Path,
			SizeBytes: size,
			Selected:  true,
		})
	}
	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *CredentialsJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	credDst := filepath.Join(target, "credentials")
	if err := os.MkdirAll(credDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-credentials.log")
	}

	// 1. Export cmdkey list for reference
	if !opts.DryRun {
		out, err := exec.Command("cmdkey.exe", "/list").CombinedOutput()
		if err == nil {
			listPath := filepath.Join(credDst, "cmdkey_list.txt")
			os.WriteFile(listPath, out, 0o644)
		}
	}

	var totalBytes int64
	var totalFiles int

	// 2. Copy vault/credential/protect directories
	for _, vd := range vaultDirs(userPath) {
		if _, err := os.Stat(vd.Path); os.IsNotExist(err) {
			continue
		}

		dirName := sanitizeName(vd.Name)
		dst := filepath.Join(credDst, dirName)

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would copy %s", vd.Name))
			continue
		}

		res, err := engine.Copy(engine.CopyOptions{
			Source:      vd.Path,
			Destination: dst,
			LogFile:     logFile,
		})
		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", vd.Name, err))
		}
	}

	result.SizeBytes = totalBytes
	result.FilesCount = totalFiles
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *CredentialsJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	credSrc := filepath.Join(source, "credentials")
	if _, err := os.Stat(credSrc); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no credential backup found")
		return result, nil
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-credentials-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	// Restore vault directories back to their original locations
	for _, vd := range vaultDirs(userPath) {
		dirName := sanitizeName(vd.Name)
		srcDir := filepath.Join(credSrc, dirName)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore %s", vd.Name))
			continue
		}

		res, err := engine.Copy(engine.CopyOptions{
			Source:      srcDir,
			Destination: vd.Path,
			LogFile:     logFile,
			ExtraFlags:  conflictFlags(opts.ConflictStrategy),
		})
		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", vd.Name, err))
		}
	}

	// Note about DPAPI
	if !opts.DryRun {
		result.Warnings = append(result.Warnings,
			"Credential vault restored. Note: DPAPI-encrypted credentials may only work if the user SID and password match the original machine.")
	}

	result.SizeBytes = totalBytes
	result.FilesCount = totalFiles
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

// credentialSummary parses cmdkey output and returns a summary string.
func credentialSummary(output string) string {
	lines := strings.Split(output, "\n")
	count := 0
	for _, line := range lines {
		if strings.Contains(line, "Target:") {
			count++
		}
	}
	return fmt.Sprintf("%d stored credentials", count)
}
