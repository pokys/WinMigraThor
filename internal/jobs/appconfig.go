//go:build windows

package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pokys/winmigrathor/internal/engine"
)

// AppConfigLocations maps app name to AppData-relative paths.
var AppConfigLocations = []struct {
	Name    string
	RelPath string // relative to APPDATA (Roaming) or LOCALAPPDATA
	Local   bool   // true = LOCALAPPDATA, false = APPDATA
}{
	{"VSCode", `Code\User`, true},
	{"VSCode extensions", `..\..\Roaming\Code\User\extensions`, true},
	{"Windows Terminal", `Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState`, true},
	{"Git Bash", `.config\git`, false},
}

// AppConfigJob backs up application configuration files.
type AppConfigJob struct{}

func (j *AppConfigJob) Name() string        { return "appconfig" }
func (j *AppConfigJob) Description() string { return "App configs (VS Code, Windows Terminal, ...)" }

func (j *AppConfigJob) Scan(userPath string) (ScanResult, error) {
	localAppData := filepath.Join(userPath, "AppData", "Local")
	appData := filepath.Join(userPath, "AppData", "Roaming")

	var items []ScanItem
	var total int64

	for _, loc := range AppConfigLocations {
		base := appData
		if loc.Local {
			base = localAppData
		}
		p := filepath.Join(base, filepath.FromSlash(loc.RelPath))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		size := folderSize(p)
		total += size
		items = append(items, ScanItem{
			Label:     loc.Name,
			Path:      p,
			SizeBytes: size,
			Selected:  false, // opt-in for advanced users
		})
	}
	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *AppConfigJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	localAppData := filepath.Join(userPath, "AppData", "Local")
	appData := filepath.Join(userPath, "AppData", "Roaming")

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-appconfig.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, loc := range AppConfigLocations {
		base := appData
		if loc.Local {
			base = localAppData
		}
		src := filepath.Join(base, filepath.FromSlash(loc.RelPath))
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}

		dst := filepath.Join(target, "appconfig", sanitizeName(loc.Name))

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would copy %s", loc.Name))
			continue
		}

		res, err := engine.Copy(engine.CopyOptions{
			Source:      src,
			Destination: dst,
			LogFile:     logFile,
		})
		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", loc.Name, err))
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

func (j *AppConfigJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	appConfigSrc := filepath.Join(source, "appconfig")
	if _, err := os.Stat(appConfigSrc); os.IsNotExist(err) {
		result.Status = "skipped"
		return result, nil
	}

	localAppData := filepath.Join(userPath, "AppData", "Local")
	appData := filepath.Join(userPath, "AppData", "Roaming")

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-appconfig-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, loc := range AppConfigLocations {
		srcDir := filepath.Join(appConfigSrc, sanitizeName(loc.Name))
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		base := appData
		if loc.Local {
			base = localAppData
		}
		dst := filepath.Join(base, filepath.FromSlash(loc.RelPath))

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore %s", loc.Name))
			continue
		}

		if err := os.MkdirAll(dst, 0o755); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("create %s dir: %v", loc.Name, err))
			continue
		}

		res, err := engine.Copy(engine.CopyOptions{
			Source:      srcDir,
			Destination: dst,
			LogFile:     logFile,
			ExtraFlags:  conflictFlags(opts.ConflictStrategy),
		})
		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", loc.Name, err))
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

func sanitizeName(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}
