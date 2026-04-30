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

// EmailJob handles Outlook PST and Thunderbird profile backup/restore.
type EmailJob struct{}

func (j *EmailJob) Name() string        { return "email" }
func (j *EmailJob) Description() string { return "Email (Outlook PST, Thunderbird)" }

// Common locations to search for PST files.
func outlookPSTLocations(userPath string) []string {
	return []string{
		filepath.Join(userPath, "Documents", "Outlook Files"),
		filepath.Join(userPath, "AppData", "Local", "Microsoft", "Outlook"),
		filepath.Join(userPath, "AppData", "Roaming", "Microsoft", "Outlook"),
		filepath.Join(userPath, "Documents"),
		userPath,
	}
}

func thunderbirdProfileDir(userPath string) string {
	return filepath.Join(userPath, "AppData", "Roaming", "Thunderbird", "Profiles")
}

func findPSTFiles(userPath string) []string {
	seen := make(map[string]bool)
	var psts []string

	for _, loc := range outlookPSTLocations(userPath) {
		entries, err := os.ReadDir(loc)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if strings.HasSuffix(lower, ".pst") || strings.HasSuffix(lower, ".ost") {
				full := filepath.Join(loc, e.Name())
				if !seen[full] {
					seen[full] = true
					psts = append(psts, full)
				}
			}
		}
	}
	return psts
}

func (j *EmailJob) Scan(userPath string) (ScanResult, error) {
	var items []ScanItem
	var total int64

	// Outlook PST files
	psts := findPSTFiles(userPath)
	for _, pst := range psts {
		info, err := os.Stat(pst)
		if err != nil {
			continue
		}
		size := info.Size()
		total += size
		items = append(items, ScanItem{
			Label:     filepath.Base(pst),
			Path:      pst,
			SizeBytes: size,
			Details:   "Outlook data file",
			Selected:  true,
		})
	}

	// Thunderbird profiles
	tbDir := thunderbirdProfileDir(userPath)
	if _, err := os.Stat(tbDir); err == nil {
		size := folderSize(tbDir)
		total += size
		items = append(items, ScanItem{
			Label:     "Thunderbird profiles",
			Path:      tbDir,
			SizeBytes: size,
			Details:   "Mozilla Thunderbird",
			Selected:  true,
		})
	}

	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *EmailJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-email.log")
	}

	var totalBytes int64
	var totalFiles int

	// Back up PST files
	psts := findPSTFiles(userPath)
	if len(psts) > 0 {
		outlookDst := filepath.Join(target, "email", "outlook")
		if err := os.MkdirAll(outlookDst, 0o755); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("create outlook dir: %v", err))
		} else {
			for _, pst := range psts {
				if opts.DryRun {
					result.Warnings = append(result.Warnings, "[dry-run] would copy: "+pst)
					continue
				}

				if err := engine.CopyFile(pst, outlookDst, logFile); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v (file may be locked)", filepath.Base(pst), err))
				} else {
					info, err := os.Stat(pst)
					if err == nil {
						totalBytes += info.Size()
					}
					totalFiles++
				}
			}
		}
	}

	// Back up Thunderbird
	tbDir := thunderbirdProfileDir(userPath)
	if _, err := os.Stat(tbDir); err == nil {
		tbDst := filepath.Join(target, "email", "thunderbird")
		if opts.DryRun {
			result.Warnings = append(result.Warnings, "[dry-run] would copy Thunderbird profiles")
		} else {
			res, err := engine.Copy(engine.CopyOptions{
				Ctx:         opts.Ctx,
				Source:      tbDir,
				Destination: tbDst,
				LogFile:     logFile,
			})
			totalBytes += res.BytesCopied
			totalFiles += res.FilesCopied
			result.Warnings = append(result.Warnings, res.Warnings...)
			if err != nil {
				result.Errors = append(result.Errors, "thunderbird: "+err.Error())
			}
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

func (j *EmailJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	emailSrc := filepath.Join(source, "email")
	if _, err := os.Stat(emailSrc); os.IsNotExist(err) {
		result.Status = "skipped"
		return result, nil
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-email-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	// Restore PST files
	outlookSrc := filepath.Join(emailSrc, "outlook")
	if _, err := os.Stat(outlookSrc); err == nil {
		outlookDst := filepath.Join(userPath, "Documents", "Outlook Files")
		if err := os.MkdirAll(outlookDst, 0o755); err == nil {
			if opts.DryRun {
				result.Warnings = append(result.Warnings, "[dry-run] would restore PST files")
			} else {
				res, err := engine.Copy(engine.CopyOptions{
					Ctx:         opts.Ctx,
					Source:      outlookSrc,
					Destination: outlookDst,
					LogFile:     logFile,
					ExtraFlags:  conflictFlags(opts.ConflictStrategy),
				})
				totalBytes += res.BytesCopied
				totalFiles += res.FilesCopied
				result.Warnings = append(result.Warnings, res.Warnings...)
				if err != nil {
					result.Errors = append(result.Errors, "outlook pst: "+err.Error())
				}
			}
		}
	}

	// Restore Thunderbird
	tbSrc := filepath.Join(emailSrc, "thunderbird")
	if _, err := os.Stat(tbSrc); err == nil {
		tbDst := thunderbirdProfileDir(userPath)
		if err := os.MkdirAll(tbDst, 0o755); err == nil {
			if opts.DryRun {
				result.Warnings = append(result.Warnings, "[dry-run] would restore Thunderbird profiles")
			} else {
				res, err := engine.Copy(engine.CopyOptions{
					Ctx:         opts.Ctx,
					Source:      tbSrc,
					Destination: tbDst,
					LogFile:     logFile,
					ExtraFlags:  conflictFlags(opts.ConflictStrategy),
				})
				totalBytes += res.BytesCopied
				totalFiles += res.FilesCopied
				result.Warnings = append(result.Warnings, res.Warnings...)
				if err != nil {
					result.Errors = append(result.Errors, "thunderbird: "+err.Error())
				}
			}
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
