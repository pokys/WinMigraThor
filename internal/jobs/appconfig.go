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

// AppConfigItem describes a config directory tied to a specific application.
// Path supports %APPDATA%, %LOCALAPPDATA% and %USERPROFILE% placeholders that
// are expanded against the target user's profile (not the current process).
type AppConfigItem struct {
	Name string
	Path string
}

// AppConfigItems is the list of safe, file-only application configs we
// migrate. Anything that needs registry, DPAPI, or a service to be running
// lives in its own job.
var AppConfigItems = []AppConfigItem{
	{"VSCode", `%APPDATA%\Code\User`},
	{"VSCode extensions", `%USERPROFILE%\.vscode\extensions`},
	{"Windows Terminal", `%LOCALAPPDATA%\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState`},
	{"Outlook signatures", `%APPDATA%\Microsoft\Signatures`},
	{"Sticky Notes", `%LOCALAPPDATA%\Packages\Microsoft.MicrosoftStickyNotes_8wekyb3d8bbwe\LocalState`},
	{"Notepad++", `%APPDATA%\Notepad++`},
}

// expandAppConfigPath resolves %APPDATA%, %LOCALAPPDATA% and %USERPROFILE%
// against userPath so the same templates work for backup (current user) and
// restore (potentially mapped to a different target user).
func expandAppConfigPath(template, userPath string) string {
	s := template
	s = strings.ReplaceAll(s, "%APPDATA%", filepath.Join(userPath, "AppData", "Roaming"))
	s = strings.ReplaceAll(s, "%LOCALAPPDATA%", filepath.Join(userPath, "AppData", "Local"))
	s = strings.ReplaceAll(s, "%USERPROFILE%", userPath)
	return filepath.Clean(s)
}

// AppConfigJob backs up application configuration files.
type AppConfigJob struct{}

func (j *AppConfigJob) Name() string        { return "appconfig" }
func (j *AppConfigJob) Description() string { return "App configs (VS Code, Outlook signatures, Sticky Notes, Notepad++, ...)" }

func (j *AppConfigJob) Scan(userPath string) (ScanResult, error) {
	var items []ScanItem
	var total int64

	for _, item := range AppConfigItems {
		p := expandAppConfigPath(item.Path, userPath)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		size := folderSize(p)
		total += size
		items = append(items, ScanItem{
			Label:     item.Name,
			Path:      p,
			SizeBytes: size,
			Selected:  false, // opt-in
		})
	}
	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *AppConfigJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-appconfig.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, item := range AppConfigItems {
		src := expandAppConfigPath(item.Path, userPath)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}

		dst := filepath.Join(target, "appconfig", sanitizeName(item.Name))

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would copy %s", item.Name))
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
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", item.Name, err))
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

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-appconfig-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, item := range AppConfigItems {
		srcDir := filepath.Join(appConfigSrc, sanitizeName(item.Name))
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		dst := expandAppConfigPath(item.Path, userPath)

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore %s", item.Name))
			continue
		}

		if err := os.MkdirAll(dst, 0o755); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("create %s dir: %v", item.Name, err))
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
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", item.Name, err))
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
