//go:build windows

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pokys/winmigrathor/internal/engine"
)

// DevEnvFiles are the dotfiles/config files to back up.
var DevEnvFiles = []string{
	".ssh",
	".gitconfig",
	".gitignore_global",
	".wslconfig",
	".npmrc",
	".yarnrc",
	".yarnrc.yml",
	".docker",
	".aws",
	".kube",
}

// DevEnvJob backs up developer environment configuration.
type DevEnvJob struct{}

func (j *DevEnvJob) Name() string        { return "devenv" }
func (j *DevEnvJob) Description() string { return "Dev environment (.ssh, .gitconfig, .docker, ...)" }

func (j *DevEnvJob) Scan(userPath string) (ScanResult, error) {
	var items []ScanItem
	var total int64

	for _, name := range DevEnvFiles {
		p := filepath.Join(userPath, name)
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		var size int64
		if info.IsDir() {
			size = folderSize(p)
		} else {
			size = info.Size()
		}
		total += size
		items = append(items, ScanItem{
			Label:     name,
			Path:      p,
			SizeBytes: size,
			Selected:  true,
		})
	}
	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *DevEnvJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	devDst := filepath.Join(target, "devenv")
	if err := os.MkdirAll(devDst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-devenv.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, name := range DevEnvFiles {
		src := filepath.Join(userPath, name)
		info, err := os.Stat(src)
		if err != nil {
			continue
		}

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would copy %s", name))
			continue
		}

		if info.IsDir() {
			dst := filepath.Join(devDst, name)
			res, err := engine.Copy(engine.CopyOptions{
				Source:      src,
				Destination: dst,
				LogFile:     logFile,
			})
			totalBytes += res.BytesCopied
			totalFiles += res.FilesCopied
			result.Warnings = append(result.Warnings, res.Warnings...)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", name, err))
			}
		} else {
			if err := engine.CopyFile(src, devDst, logFile); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", name, err))
			} else {
				totalBytes += info.Size()
				totalFiles++
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

func (j *DevEnvJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	devSrc := filepath.Join(source, "devenv")
	if _, err := os.Stat(devSrc); os.IsNotExist(err) {
		result.Status = "skipped"
		return result, nil
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-devenv-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	entries, err := os.ReadDir(devSrc)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	for _, e := range entries {
		src := filepath.Join(devSrc, e.Name())
		dst := filepath.Join(userPath, e.Name())

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore %s", e.Name()))
			continue
		}

		if e.IsDir() {
			res, err := engine.Copy(engine.CopyOptions{
				Source:      src,
				Destination: dst,
				LogFile:     logFile,
				ExtraFlags:  conflictFlags(opts.ConflictStrategy),
			})
			totalBytes += res.BytesCopied
			totalFiles += res.FilesCopied
			result.Warnings = append(result.Warnings, res.Warnings...)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
			}
		} else {
			if err := engine.CopyFile(src, userPath, logFile); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
			} else {
				info, _ := e.Info()
				if info != nil {
					totalBytes += info.Size()
				}
				totalFiles++
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
