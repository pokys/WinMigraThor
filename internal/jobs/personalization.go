//go:build windows

package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

)

// PersonalizationJob backs up and restores Windows personalization settings:
// wallpaper, theme/colors, dark mode, accent color, keyboard layouts, regional settings.
type PersonalizationJob struct{}

func (j *PersonalizationJob) Name() string        { return "personalization" }
func (j *PersonalizationJob) Description() string { return "Wallpaper, theme, dark mode, keyboard layouts" }

type personalSettings struct {
	Wallpaper       string `json:"wallpaper"`        // file name (copied separately)
	DarkMode        int    `json:"dark_mode"`         // 0=dark, 1=light (AppsUseLightTheme)
	SystemDarkMode  int    `json:"system_dark_mode"`  // 0=dark, 1=light (SystemUsesLightTheme)
	AccentColor     string `json:"accent_color"`      // DWORD hex
	ColorPrevalence int    `json:"color_prevalence"`  // show accent on title bars/start
	Locale          string `json:"locale"`            // e.g. "cs-CZ"
	KeyboardLayouts string `json:"keyboard_layouts"`  // semicolon-separated list
}

func (j *PersonalizationJob) Scan(userPath string) (ScanResult, error) {
	items := []ScanItem{
		{Label: "Wallpaper", Details: "Desktop background image", Selected: true},
		{Label: "Theme", Details: "Dark/light mode, accent color", Selected: true},
		{Label: "Keyboard", Details: "Input languages and layouts", Selected: true},
	}
	return ScanResult{Items: items, TotalSizeBytes: 1024 * 100}, nil // rough estimate
}

func (j *PersonalizationJob) Backup(userPath, target string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	dstDir := filepath.Join(target, "personalization")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would export personalization settings")
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	settings := personalSettings{}

	// Wallpaper path
	wpPath := readRegString(`HKCU\Control Panel\Desktop`, "WallPaper")
	if wpPath != "" && fileExists(wpPath) {
		settings.Wallpaper = filepath.Base(wpPath)
		wpData, err := os.ReadFile(wpPath)
		if err != nil {
			result.Warnings = append(result.Warnings, "wallpaper read: "+err.Error())
		} else if err := os.WriteFile(filepath.Join(dstDir, settings.Wallpaper), wpData, 0o644); err != nil {
			result.Warnings = append(result.Warnings, "wallpaper write: "+err.Error())
		} else {
			result.SizeBytes += int64(len(wpData))
			result.FilesCount++
		}
	}

	// Dark mode
	settings.DarkMode = readRegDWORD(`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "AppsUseLightTheme")
	settings.SystemDarkMode = readRegDWORD(`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "SystemUsesLightTheme")
	settings.ColorPrevalence = readRegDWORD(`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "ColorPrevalence")

	// Accent color
	settings.AccentColor = readRegString(`HKCU\SOFTWARE\Microsoft\Windows\DWM`, "AccentColor")

	// Locale
	settings.Locale = readRegString(`HKCU\Control Panel\International`, "LocaleName")

	// Keyboard layouts
	settings.KeyboardLayouts = getKeyboardLayouts()

	// Save settings JSON
	data, _ := json.MarshalIndent(settings, "", "  ")
	jsonPath := filepath.Join(dstDir, "personalization.json")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		result.Errors = append(result.Errors, "write settings: "+err.Error())
	} else {
		result.SizeBytes += int64(len(data))
		result.FilesCount++
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func (j *PersonalizationJob) Restore(source, userPath string, opts Options) (Result, error) {
	start := time.Now()
	result := Result{JobName: j.Name()}

	srcDir := filepath.Join(source, "personalization")
	jsonPath := filepath.Join(srcDir, "personalization.json")

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		result.Status = "skipped"
		result.Warnings = append(result.Warnings, "no personalization backup found")
		return result, nil
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "read settings: "+err.Error())
		return result, err
	}

	var settings personalSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		result.Status = "error"
		result.Errors = append(result.Errors, "parse settings: "+err.Error())
		return result, err
	}

	if opts.DryRun {
		result.Warnings = append(result.Warnings, "[dry-run] would restore personalization settings")
		result.Status = "success"
		if opts.ProgressCh != nil {
			opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
		}
		return result, nil
	}

	// Restore wallpaper
	if settings.Wallpaper != "" {
		wpSrc := filepath.Join(srcDir, settings.Wallpaper)
		if fileExists(wpSrc) {
			// Copy to user's Pictures folder and set as wallpaper
			wpDst := filepath.Join(userPath, "Pictures", settings.Wallpaper)
			if data, err := os.ReadFile(wpSrc); err == nil {
				os.MkdirAll(filepath.Dir(wpDst), 0o755)
				if err := os.WriteFile(wpDst, data, 0o644); err == nil {
					writeRegString(`HKCU\Control Panel\Desktop`, "WallPaper", wpDst)
					// Apply wallpaper immediately
					runPS(fmt.Sprintf(`Add-Type -TypeDefinition 'using System.Runtime.InteropServices; public class W { [DllImport("user32.dll")] public static extern int SystemParametersInfo(int a,int b,string c,int d); }'; [W]::SystemParametersInfo(0x0014,0,'%s',0x01|0x02)`, escapeSingleQuote(wpDst)))
					result.FilesCount++
				}
			}
		}
	}

	// Restore dark mode
	writeRegDWORD(`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "AppsUseLightTheme", settings.DarkMode)
	writeRegDWORD(`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "SystemUsesLightTheme", settings.SystemDarkMode)
	writeRegDWORD(`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "ColorPrevalence", settings.ColorPrevalence)

	// Restore accent color
	if settings.AccentColor != "" {
		writeRegString(`HKCU\SOFTWARE\Microsoft\Windows\DWM`, "AccentColor", settings.AccentColor)
	}

	// Restore locale
	if settings.Locale != "" {
		runPS(fmt.Sprintf(`Set-WinUserLanguageList -LanguageList '%s' -Force`, escapeSingleQuote(settings.Locale)))
	}

	// Restore keyboard layouts
	if settings.KeyboardLayouts != "" {
		restoreKeyboardLayouts(settings.KeyboardLayouts)
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

// ── Registry helpers ────────────────────────────────────────────────────────

func readRegString(keyPath, valueName string) string {
	ps := fmt.Sprintf(`(Get-ItemProperty -Path 'Registry::%s' -Name '%s' -ErrorAction SilentlyContinue).'%s'`,
		keyPath, valueName, valueName)
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func readRegDWORD(keyPath, valueName string) int {
	s := readRegString(keyPath, valueName)
	if s == "" {
		return -1
	}
	var v int
	fmt.Sscanf(s, "%d", &v)
	return v
}

func writeRegString(keyPath, valueName, value string) {
	ps := fmt.Sprintf(`Set-ItemProperty -Path 'Registry::%s' -Name '%s' -Value '%s' -Force`,
		keyPath, valueName, escapeSingleQuote(value))
	exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Run()
}

func writeRegDWORD(keyPath, valueName string, value int) {
	if value < 0 {
		return // skip unknown values
	}
	ps := fmt.Sprintf(`Set-ItemProperty -Path 'Registry::%s' -Name '%s' -Value %d -Type DWord -Force`,
		keyPath, valueName, value)
	exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Run()
}

func getKeyboardLayouts() string {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command",
		`(Get-WinUserLanguageList).LanguageTag -join ';'`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func restoreKeyboardLayouts(layouts string) {
	tags := strings.Split(layouts, ";")
	var list []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			list = append(list, fmt.Sprintf("'%s'", escapeSingleQuote(t)))
		}
	}
	if len(list) == 0 {
		return
	}
	ps := fmt.Sprintf(`Set-WinUserLanguageList -LanguageList %s -Force`, strings.Join(list, ","))
	exec.Command("powershell.exe", "-NoProfile", "-Command", ps).Run()
}

func runPS(script string) {
	exec.Command("powershell.exe", "-NoProfile", "-Command", script).Run()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
