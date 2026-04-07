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

// Browser represents a detected browser installation.
type Browser struct {
	Name       string
	ProfileDir string   // base profile directory
	Profiles   []string // detected profile subdirs
}

var firefoxConfigFiles = []string{
	"profiles.ini",
	"installs.ini",
}

// BrowsersJob handles browser profile backup/restore.
type BrowsersJob struct{}

func (j *BrowsersJob) Name() string        { return "browsers" }
func (j *BrowsersJob) Description() string { return "Browser profiles (Chrome, Edge, Firefox)" }

// browserLocations maps browser name to AppData-relative path.
var browserLocations = []struct {
	Name    string
	RelPath string // relative to LOCALAPPDATA
}{
	{"Chrome", `Google\Chrome\User Data`},
	{"Edge", `Microsoft\Edge\User Data`},
	{"Firefox", `..\Roaming\Mozilla\Firefox\Profiles`},
}

// ExcludeBrowserDirs are cache/temp dirs to exclude from browser backup.
var ExcludeBrowserDirs = []string{
	"Cache",
	"Code Cache",
	"GPUCache",
	"ShaderCache",
	"Service Worker",
	"CacheStorage",
	"ScriptCache",
	"Crashpad",
	"crash_inspector",
}

func detectBrowsers(userPath string) []Browser {
	localAppData := filepath.Join(userPath, "AppData", "Local")
	var browsers []Browser

	for _, loc := range browserLocations {
		profileBase := filepath.Join(localAppData, filepath.FromSlash(loc.RelPath))
		if _, err := os.Stat(profileBase); os.IsNotExist(err) {
			continue
		}

		b := Browser{Name: loc.Name, ProfileDir: profileBase}

		if loc.Name == "Firefox" {
			// Firefox: each subdir is a profile
			entries, err := os.ReadDir(profileBase)
			if err == nil {
				for _, e := range entries {
					if e.IsDir() {
						b.Profiles = append(b.Profiles, e.Name())
					}
				}
			}
		} else {
			// Chrome/Edge: profiles are "Default" + "Profile N" dirs
			entries, err := os.ReadDir(profileBase)
			if err == nil {
				for _, e := range entries {
					if !e.IsDir() {
						continue
					}
					name := e.Name()
					if name == "Default" || strings.HasPrefix(name, "Profile ") {
						b.Profiles = append(b.Profiles, name)
					}
				}
			}
		}

		if len(b.Profiles) > 0 {
			browsers = append(browsers, b)
		}
	}
	return browsers
}

func (j *BrowsersJob) Scan(userPath string) (ScanResult, error) {
	browsers := detectBrowsers(userPath)
	var items []ScanItem
	var total int64

	for _, b := range browsers {
		for _, p := range b.Profiles {
			profilePath := filepath.Join(b.ProfileDir, p)
			size := folderSize(profilePath)
			total += size
			items = append(items, ScanItem{
				Label:     fmt.Sprintf("%s - %s", b.Name, p),
				Path:      profilePath,
				SizeBytes: size,
				Selected:  true,
			})
		}
	}
	return ScanResult{Items: items, TotalSizeBytes: total}, nil
}

func (j *BrowsersJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	browsers := detectBrowsers(userPath)

	// Filter browsers by selection if specified
	if len(opts.SelectedBrowsers) > 0 {
		selected := make(map[string]bool)
		for _, name := range opts.SelectedBrowsers {
			selected[name] = true
		}
		var filtered []Browser
		for _, b := range browsers {
			if selected[b.Name] {
				filtered = append(filtered, b)
			}
		}
		browsers = filtered
	}

	if len(browsers) == 0 {
		result.Status = "skipped"
		return result, nil
	}

	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-browsers.log")
	}

	var totalBytes int64
	var totalFiles int

	for _, b := range browsers {
		browserDst := filepath.Join(target, "browsers", strings.ToLower(b.Name))

		if b.Name == "Firefox" && !opts.DryRun {
			if err := backupFirefoxConfig(b.ProfileDir, browserDst); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Firefox config: %v", err))
			}
		}

		for _, profile := range b.Profiles {
			src := filepath.Join(b.ProfileDir, profile)
			dst := filepath.Join(browserDst, profile)

			if opts.DryRun {
				result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would copy %s → %s", src, dst))
				continue
			}

			excludeDirs := append(engine.ExcludeDirs, ExcludeBrowserDirs...)

			res, err := engine.Copy(engine.CopyOptions{
				Source:      src,
				Destination: dst,
				LogFile:     logFile,
				ExtraFlags:  buildExcludeFlags(excludeDirs),
			})
			totalBytes += res.BytesCopied
			totalFiles += res.FilesCopied
			result.Warnings = append(result.Warnings, res.Warnings...)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s/%s: %v", b.Name, profile, err))
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

func (j *BrowsersJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	browsersDir := filepath.Join(source, "browsers")
	if _, err := os.Stat(browsersDir); os.IsNotExist(err) {
		result.Status = "skipped"
		return result, nil
	}

	localAppData := filepath.Join(userPath, "AppData", "Local")
	logFile := ""
	if opts.LogDir != "" {
		logFile = filepath.Join(opts.LogDir, "robocopy-browsers-restore.log")
	}

	var totalBytes int64
	var totalFiles int

	entries, err := os.ReadDir(browsersDir)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	// Build selected browsers filter
	selectedFilter := make(map[string]bool)
	if len(opts.SelectedBrowsers) > 0 {
		for _, name := range opts.SelectedBrowsers {
			selectedFilter[strings.ToLower(name)] = true
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		browserName := e.Name()

		// Filter by selected browsers
		if len(selectedFilter) > 0 && !selectedFilter[strings.ToLower(browserName)] {
			continue
		}

		// Map back to original location
		var dstBase string
		switch strings.ToLower(browserName) {
		case "chrome":
			dstBase = filepath.Join(localAppData, `Google\Chrome\User Data`)
		case "edge":
			dstBase = filepath.Join(localAppData, `Microsoft\Edge\User Data`)
		case "firefox":
			dstBase = filepath.Join(localAppData, `..\Roaming\Mozilla\Firefox\Profiles`)
		default:
			continue
		}

		srcDir := filepath.Join(browsersDir, browserName)

		if strings.EqualFold(browserName, "firefox") && !opts.DryRun {
			if err := restoreFirefoxConfig(srcDir, dstBase); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s config: %v", browserName, err))
			}
		}

		if opts.DryRun {
			result.Warnings = append(result.Warnings, fmt.Sprintf("[dry-run] would restore %s → %s", srcDir, dstBase))
			continue
		}

		res, err := engine.Copy(engine.CopyOptions{
			Source:      srcDir,
			Destination: dstBase,
			LogFile:     logFile,
			ExtraFlags:  conflictFlags(opts.ConflictStrategy),
		})
		totalBytes += res.BytesCopied
		totalFiles += res.FilesCopied
		result.Warnings = append(result.Warnings, res.Warnings...)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", browserName, err))
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

func backupFirefoxConfig(profileDir, browserDst string) error {
	firefoxRoot := filepath.Clean(filepath.Join(profileDir, ".."))
	configDst := filepath.Join(browserDst, "_config")
	if err := os.MkdirAll(configDst, 0o755); err != nil {
		return err
	}

	for _, name := range firefoxConfigFiles {
		src := filepath.Join(firefoxRoot, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(configDst, name)); err != nil {
			return err
		}
	}
	return nil
}

func restoreFirefoxConfig(srcDir, profilesDir string) error {
	configSrc := filepath.Join(srcDir, "_config")
	if _, err := os.Stat(configSrc); err != nil {
		return nil
	}

	firefoxRoot := filepath.Clean(filepath.Join(profilesDir, ".."))
	if err := os.MkdirAll(firefoxRoot, 0o755); err != nil {
		return err
	}

	for _, name := range firefoxConfigFiles {
		src := filepath.Join(configSrc, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(firefoxRoot, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Close()
}

func buildExcludeFlags(dirs []string) []string {
	if len(dirs) == 0 {
		return nil
	}
	flags := []string{"/XD"}
	flags = append(flags, dirs...)
	return flags
}

func statusFromResult(errors, warnings []string) string {
	if len(errors) > 0 {
		return "error"
	}
	if len(warnings) > 0 {
		return "warning"
	}
	return "success"
}
