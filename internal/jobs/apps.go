//go:build windows

package jobs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// AppInfo describes an installed application.
type AppInfo struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	Publisher      string `json:"publisher,omitempty"`
	InstallLocation string `json:"install_location,omitempty"`
	WingetID       string `json:"winget_id,omitempty"`
	MatchQuality   string `json:"match_quality"` // exact, partial, none
}

// WingetEntry is saved to apps_winget.json.
type WingetEntry struct {
	WingetID     string `json:"winget_id"`
	Name         string `json:"name"`
	MatchQuality string `json:"match_quality"`
}

// AppsJob detects installed apps and generates reinstall scripts.
type AppsJob struct{}

func (j *AppsJob) Name() string        { return "apps" }
func (j *AppsJob) Description() string { return "Installed apps (detect + winget match)" }

// registryPaths are scanned for installed applications.
var registryPaths = []struct {
	hive registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, `Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
}

func (j *AppsJob) Scan(userPath string) (ScanResult, error) {
	apps := scanRegistry()
	var items []ScanItem
	for _, app := range apps {
		items = append(items, ScanItem{
			Label:   app.Name,
			Details: app.Version,
		})
	}
	return ScanResult{Items: items}, nil
}

func scanRegistry() []AppInfo {
	seen := make(map[string]bool)
	var apps []AppInfo

	for _, rp := range registryPaths {
		k, err := registry.OpenKey(rp.hive, rp.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}

		subkeys, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}

		for _, subkey := range subkeys {
			sk, err := registry.OpenKey(rp.hive, rp.path+`\`+subkey, registry.QUERY_VALUE)
			if err != nil {
				continue
			}

			name, _, _ := sk.GetStringValue("DisplayName")
			version, _, _ := sk.GetStringValue("DisplayVersion")
			publisher, _, _ := sk.GetStringValue("Publisher")
			location, _, _ := sk.GetStringValue("InstallLocation")
			systemComponent, _, _ := sk.GetIntegerValue("SystemComponent")
			sk.Close()

			if name == "" || systemComponent == 1 {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true

			apps = append(apps, AppInfo{
				Name:            name,
				Version:         version,
				Publisher:       publisher,
				InstallLocation: location,
				MatchQuality:    "none",
			})
		}
	}
	return apps
}

func matchWithWinget(apps []AppInfo) []AppInfo {
	// Try to get winget list output
	out, err := exec.Command("winget.exe", "list", "--source", "winget", "--output", "json").Output()
	if err != nil {
		// Fallback: parse table output
		out, err = exec.Command("winget.exe", "list").Output()
		if err != nil {
			return apps
		}
		return matchFromTableOutput(apps, string(out))
	}

	// Parse JSON output
	var wingetList []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(out, &wingetList); err != nil {
		return apps
	}

	wingetMap := make(map[string]string) // name (lower) -> ID
	for _, w := range wingetList {
		wingetMap[strings.ToLower(w.Name)] = w.ID
	}

	for i, app := range apps {
		lower := strings.ToLower(app.Name)
		if id, ok := wingetMap[lower]; ok {
			apps[i].WingetID = id
			apps[i].MatchQuality = "exact"
		}
	}
	return apps
}

func matchFromTableOutput(apps []AppInfo, output string) []AppInfo {
	lines := strings.Split(output, "\n")
	// Build a map from app name (lower) to winget ID from the table
	wingetMap := make(map[string]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "Name") {
			continue
		}
		// Table format: Name  Id  Version  Available  Source
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			wingetMap[strings.ToLower(fields[0])] = fields[1]
		}
	}

	for i, app := range apps {
		lower := strings.ToLower(app.Name)
		if id, ok := wingetMap[lower]; ok {
			apps[i].WingetID = id
			apps[i].MatchQuality = "exact"
		} else {
			// Try partial match
			for wName, wID := range wingetMap {
				if strings.Contains(lower, wName) || strings.Contains(wName, lower) {
					apps[i].WingetID = wID
					apps[i].MatchQuality = "partial"
					break
				}
			}
		}
	}
	return apps
}

func (j *AppsJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would scan registry for installed apps")
		result.Status = "success"
		return result, nil
	}

	apps := scanRegistry()
	apps = matchWithWinget(apps)

	// Save apps.json
	appsJSON := filepath.Join(target, "apps.json")
	if err := writeJSON(appsJSON, apps); err != nil {
		result.Errors = append(result.Errors, "write apps.json: "+err.Error())
	}

	// Save apps_winget.json (only matched apps)
	var wingetApps []WingetEntry
	for _, app := range apps {
		if app.WingetID != "" {
			wingetApps = append(wingetApps, WingetEntry{
				WingetID:     app.WingetID,
				Name:         app.Name,
				MatchQuality: app.MatchQuality,
			})
		}
	}
	wingetJSON := filepath.Join(target, "apps_winget.json")
	if err := writeJSON(wingetJSON, wingetApps); err != nil {
		result.Errors = append(result.Errors, "write apps_winget.json: "+err.Error())
	}

	result.FilesCount = len(apps)
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *AppsJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	wingetJSONPath := filepath.Join(source, "apps_winget.json")
	if _, err := os.Stat(wingetJSONPath); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no apps_winget.json found")
		return result, nil
	}

	var wingetApps []WingetEntry
	if err := readJSON(wingetJSONPath, &wingetApps); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "read apps_winget.json: "+err.Error())
		return result, err
	}

	// Generate reinstall.ps1
	scriptPath := filepath.Join(userPath, "reinstall.ps1")
	if err := generateReinstallScript(scriptPath, wingetApps); err != nil {
		result.Errors = append(result.Errors, "generate reinstall script: "+err.Error())
	} else {
		result.Warnings = append(result.Warnings, fmt.Sprintf("reinstall.ps1 generated at %s (%d apps)", scriptPath, len(wingetApps)))
	}

	result.FilesCount = len(wingetApps)
	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func generateReinstallScript(path string, apps []WingetEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "# Auto-generated by MigraThor - review before running\n")
	fmt.Fprintf(w, "# Date: %s\n\n", time.Now().Format("2006-01-02"))

	for _, app := range apps {
		fmt.Fprintf(w, "winget install --id %s -e --accept-package-agreements --accept-source-agreements\n", app.WingetID)
	}
	return w.Flush()
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}
