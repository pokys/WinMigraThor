//go:build windows

package jobs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

type wingetCandidate struct {
	Name string
	ID   string
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
	cache := make(map[string]AppInfo)
	for i, app := range apps {
		key := normalizeWingetText(app.Name)
		if key == "" {
			continue
		}
		if cached, ok := cache[key]; ok {
			apps[i].WingetID = cached.WingetID
			apps[i].MatchQuality = cached.MatchQuality
			continue
		}

		matched := app
		id, quality := findWingetMatch(app)
		if id != "" {
			matched.WingetID = id
			matched.MatchQuality = quality
		}
		cache[key] = matched
		apps[i].WingetID = matched.WingetID
		apps[i].MatchQuality = matched.MatchQuality
	}
	return apps
}

var wingetTableRowRE = regexp.MustCompile(`^(.*?)\s{2,}(\S+)\s{2,}.*$`)

func findWingetMatch(app AppInfo) (string, string) {
	candidates := searchWingetCatalog(app.Name)
	if len(candidates) == 0 && app.Publisher != "" {
		candidates = searchWingetCatalog(app.Publisher + " " + app.Name)
	}
	if len(candidates) == 0 {
		return "", "none"
	}

	best, ok := rankWingetCandidate(app, candidates)
	if !ok {
		return "", "none"
	}
	return best.ID, best.MatchQuality
}

func searchWingetCatalog(query string) []wingetCandidate {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	args := []string{"search", "--source", "winget", "--name", query}
	out, err := exec.Command("winget.exe", args...).Output()
	if err != nil {
		out, err = exec.Command("winget.exe", "search", query, "--source", "winget").Output()
		if err != nil {
			return nil
		}
	}

	candidates := parseWingetTableCandidates(string(out))
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Name == candidates[j].Name {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates
}

func parseWingetTableCandidates(output string) []wingetCandidate {
	lines := strings.Split(output, "\n")
	seen := make(map[string]bool)
	var candidates []wingetCandidate
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "Name") || strings.HasPrefix(line, "No package found") {
			continue
		}
		matches := wingetTableRowRE.FindStringSubmatch(line)
		if len(matches) == 3 {
			name := strings.TrimSpace(matches[1])
			id := strings.TrimSpace(matches[2])
			if name != "" && id != "" {
				key := strings.ToLower(name) + "\x00" + strings.ToLower(id)
				if !seen[key] {
					seen[key] = true
					candidates = append(candidates, wingetCandidate{Name: name, ID: id})
				}
			}
		}
	}
	return candidates
}

func rankWingetCandidate(app AppInfo, candidates []wingetCandidate) (struct {
	ID           string
	MatchQuality string
}, bool) {
	type scored struct {
		wingetCandidate
		score        int
		matchQuality string
	}

	targetName := normalizeWingetText(app.Name)
	targetPublisher := normalizeWingetText(app.Publisher)

	var best scored
	best.score = -1

	for _, candidate := range candidates {
		candidateName := normalizeWingetText(candidate.Name)
		candidateID := normalizeWingetText(candidate.ID)
		if candidateName == "" || candidateID == "" {
			continue
		}

		score := 0
		quality := "none"

		switch {
		case candidateName == targetName:
			score = 100
			quality = "exact"
		case strings.Contains(candidateName, targetName) || strings.Contains(targetName, candidateName):
			score = 60
			quality = "partial"
		default:
			shared := sharedWingetTokens(targetName, candidateName)
			if shared > 0 {
				score = shared * 10
				quality = "partial"
			}
		}

		if score > 0 && targetPublisher != "" {
			if strings.Contains(candidateName, targetPublisher) || strings.Contains(candidateID, targetPublisher) {
				score += 15
			}
		}

		if score > best.score || (score == best.score && len(candidateName) < len(normalizeWingetText(best.Name))) {
			best = scored{
				wingetCandidate: candidate,
				score:           score,
				matchQuality:    quality,
			}
		}
	}

	if best.score >= 100 {
		return struct {
			ID           string
			MatchQuality string
		}{ID: best.ID, MatchQuality: "exact"}, true
	}
	if best.score >= 60 {
		return struct {
			ID           string
			MatchQuality string
		}{ID: best.ID, MatchQuality: "partial"}, true
	}
	return struct {
		ID           string
		MatchQuality string
	}{}, false
}

func normalizeWingetText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"-", " ",
		"_", " ",
		".", " ",
		",", " ",
		"/", " ",
		"\\", " ",
		"'", "",
		`"`, "",
	)
	value = replacer.Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func sharedWingetTokens(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	tokensA := strings.Fields(a)
	tokensB := strings.Fields(b)
	setB := make(map[string]bool, len(tokensB))
	for _, token := range tokensB {
		setB[token] = true
	}
	shared := 0
	for _, token := range tokensA {
		if len(token) < 3 {
			continue
		}
		if setB[token] {
			shared++
		}
	}
	return shared
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
	wingetApps := make([]WingetEntry, 0)
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
