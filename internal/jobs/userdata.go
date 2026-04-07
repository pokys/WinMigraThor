//go:build windows

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pokys/winmigrathor/internal/engine"
)

// StandardFolders are the default user data folders to back up.
var StandardFolders = []string{
	"Desktop",
	"Documents",
	"Downloads",
	"Pictures",
	"Videos",
	"Music",
}

// UserDataJob backs up and restores standard user folders.
type UserDataJob struct{}

func (j *UserDataJob) Name() string        { return "userdata" }
func (j *UserDataJob) Description() string { return "User folders (Desktop, Documents, Downloads, ...)" }

func (j *UserDataJob) Scan(userPath string) (ScanResult, error) {
	var items []ScanItem
	var total int64

	for _, folder := range StandardFolders {
		p := filepath.Join(userPath, folder)
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		size := folderSize(p)
		total += size
		items = append(items, ScanItem{
			Label:     folder,
			Path:      p,
			SizeBytes: size,
			Selected:  true,
		})
	}
	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *UserDataJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	username := filepath.Base(userPath)
	dst := filepath.Join(target, "users", username)

	if err := os.MkdirAll(dst, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, fmt.Sprintf("create target dir: %v", err))
		return result, err
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-userdata.log")
	}

	var totalBytes int64
	var totalFiles int

	// Determine which folders to back up
	folders := StandardFolders
	if len(opts.SelectedFolders) > 0 {
		folders = opts.SelectedFolders
	}

	for _, folder := range folders {
		src := filepath.Join(userPath, folder)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		folderDst := filepath.Join(dst, folder)

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would copy %s → %s", src, folderDst))
			continue
		}

		var progressCh chan engine.CopyProgress
		if opts.ProgressCh != nil {
			progressCh = make(chan engine.CopyProgress, 100)
			go func(name string, ch <-chan engine.CopyProgress) {
				for p := range ch {
					opts.ProgressCh <- Progress{
						JobName:     j.Name(),
						CurrentFile: fmt.Sprintf("%s/%s", name, p.CurrentFile),
						Warning:     p.Warning,
					}
				}
			}(folder, progressCh)
		}

		res, err := engine.Copy(engine.CopyOptions{
			Source:      src,
			Destination: folderDst,
			LogFile:     logFile,
			ProgressCh:  progressCh,
		})
		if progressCh != nil {
			close(progressCh)
		}

		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", folder, err))
		}
	}

	result.SizeBytes = totalBytes
	result.FilesCount = totalFiles
	result.Duration = time.Since(start).Round(time.Second).String()

	if len(result.Errors) > 0 {
		result.Status = "error"
	} else if len(result.Warnings) > 0 {
		result.Status = "warning"
	} else {
		result.Status = "success"
	}

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}

	return result, nil
}

func (j *UserDataJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	username := filepath.Base(userPath)
	src := filepath.Join(source, "users", username)

	if _, err := os.Stat(src); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no userdata backup found for "+username)
		return result, nil
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-userdata-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, folder := range StandardFolders {
		folderSrc := filepath.Join(src, folder)
		if _, err := os.Stat(folderSrc); os.IsNotExist(err) {
			continue
		}
		folderDst := filepath.Join(userPath, folder)

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore %s → %s", folderSrc, folderDst))
			continue
		}

		extraFlags := conflictFlags(opts.ConflictStrategy)

		res, err := engine.Copy(engine.CopyOptions{
			Source:      folderSrc,
			Destination: folderDst,
			LogFile:     logFile,
			ExtraFlags:  extraFlags,
		})

		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", folder, err))
		}
	}

	result.SizeBytes = totalBytes
	result.FilesCount = totalFiles
	result.Duration = time.Since(start).Round(time.Second).String()

	if len(result.Errors) > 0 {
		result.Status = "error"
	} else if len(result.Warnings) > 0 {
		result.Status = "warning"
	} else {
		result.Status = "success"
	}

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}

	return result, nil
}

func conflictFlags(strategy string) []string {
	switch strategy {
	case "overwrite":
		return []string{"/IS", "/IT"} // include same/tweaked
	case "skip":
		return []string{"/XN", "/XO"} // exclude newer/older
	case "rename":
		return nil // handled at higher level
	default:
		return nil
	}
}

func folderSize(path string) int64 {
	var size int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			size += info.Size()
		}
		return nil
	})
	return size
}
