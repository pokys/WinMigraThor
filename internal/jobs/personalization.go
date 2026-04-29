//go:build windows

package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// PersonalizationJob backs up and restores low-risk Windows personalization:
// wallpaper, dark mode and accent color. Locale and keyboard layouts are
// intentionally not migrated — they depend on installed language packs on the
// target machine and getting them wrong can break Explorer / input.
type PersonalizationJob struct{}

func (j *PersonalizationJob) Name() string        { return "personalization" }
func (j *PersonalizationJob) Description() string { return "Wallpaper, dark mode, accent color" }

type personalSettings struct {
	Wallpaper       string `json:"wallpaper"`         // file name (copied separately)
	WallpaperStyle  string `json:"wallpaper_style"`   // e.g. "10" = Fill
	TileWallpaper   string `json:"tile_wallpaper"`    // "0" or "1"
	DarkMode        int    `json:"dark_mode"`         // 1=light, 0=dark (AppsUseLightTheme)
	SystemDarkMode  int    `json:"system_dark_mode"`  // SystemUsesLightTheme
	AccentColor     uint32 `json:"accent_color"`      // DWORD ABGR
	ColorPrevalence int    `json:"color_prevalence"`  // accent on title bars/start
}

const (
	spiSetDeskWallpaper = 0x0014
	spifUpdateIniFile   = 0x01
	spifSendChange      = 0x02
)

func (j *PersonalizationJob) Scan(userPath string) (ScanResult, error) {
	items := []ScanItem{
		{Label: "Wallpaper", Details: "Desktop background image", Selected: true},
		{Label: "Theme", Details: "Dark/light mode, accent color", Selected: true},
	}
	return ScanResult{Items: items, TotalSizeBytes: 1024 * 100}, nil
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

	desktop, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.QUERY_VALUE)
	if err == nil {
		wpPath, _, _ := desktop.GetStringValue("WallPaper")
		settings.WallpaperStyle, _, _ = desktop.GetStringValue("WallpaperStyle")
		settings.TileWallpaper, _, _ = desktop.GetStringValue("TileWallpaper")
		desktop.Close()

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
	} else {
		result.Warnings = append(result.Warnings, "open Control Panel\\Desktop: "+err.Error())
	}

	if personalize, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE); err == nil {
		settings.DarkMode = readDWORD(personalize, "AppsUseLightTheme", -1)
		settings.SystemDarkMode = readDWORD(personalize, "SystemUsesLightTheme", -1)
		settings.ColorPrevalence = readDWORD(personalize, "ColorPrevalence", -1)
		personalize.Close()
	}

	if dwm, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\DWM`, registry.QUERY_VALUE); err == nil {
		if v, _, err := dwm.GetIntegerValue("AccentColor"); err == nil {
			settings.AccentColor = uint32(v)
		}
		dwm.Close()
	}

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

	// Wallpaper: copy file into the user's Themes folder, set style hints in
	// HKCU, then call SystemParametersInfoW to apply it. Windows regenerates
	// TranscodedWallpaper itself — we don't touch it (writing the wrong format
	// there leaves the desktop blank on next login).
	if settings.Wallpaper != "" {
		wpSrc := filepath.Join(srcDir, settings.Wallpaper)
		if fileExists(wpSrc) {
			wpData, err := os.ReadFile(wpSrc)
			if err != nil {
				result.Warnings = append(result.Warnings, "wallpaper read: "+err.Error())
			} else {
				appData := os.Getenv("APPDATA")
				themeDir := filepath.Join(appData, `Microsoft\Windows\Themes`)
				if err := os.MkdirAll(themeDir, 0o755); err != nil {
					result.Warnings = append(result.Warnings, "create theme dir: "+err.Error())
				}
				wpDst := filepath.Join(themeDir, settings.Wallpaper)
				if err := os.WriteFile(wpDst, wpData, 0o644); err != nil {
					result.Warnings = append(result.Warnings, "wallpaper write: "+err.Error())
				} else {
					style := settings.WallpaperStyle
					if style == "" {
						style = "10" // Fill
					}
					tile := settings.TileWallpaper
					if tile == "" {
						tile = "0"
					}
					if err := writeDesktopRegistry(wpDst, style, tile); err != nil {
						result.Warnings = append(result.Warnings, "wallpaper registry: "+err.Error())
					}
					if err := applyWallpaper(wpDst); err != nil {
						result.Warnings = append(result.Warnings, "apply wallpaper: "+err.Error())
					}
					result.FilesCount++
				}
			}
		} else {
			result.Warnings = append(result.Warnings, "wallpaper file not found in backup: "+settings.Wallpaper)
		}
	}

	if personalize, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.SET_VALUE); err == nil {
		writeDWORD(personalize, "AppsUseLightTheme", settings.DarkMode)
		writeDWORD(personalize, "SystemUsesLightTheme", settings.SystemDarkMode)
		writeDWORD(personalize, "ColorPrevalence", settings.ColorPrevalence)
		personalize.Close()
	} else {
		result.Warnings = append(result.Warnings, "open Personalize: "+err.Error())
	}

	if settings.AccentColor != 0 {
		if dwm, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\DWM`, registry.SET_VALUE); err == nil {
			dwm.SetDWordValue("AccentColor", settings.AccentColor)
			dwm.Close()
		}
	}

	result.Duration = time.Since(start).Round(time.Second).String()
	result.Status = statusFromResult(result.Errors, result.Warnings)

	if opts.ProgressCh != nil {
		opts.ProgressCh <- Progress{JobName: j.Name(), Done: true}
	}
	return result, nil
}

func writeDesktopRegistry(wallpaperPath, style, tile string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("WallPaper", wallpaperPath); err != nil {
		return err
	}
	if err := k.SetStringValue("WallpaperStyle", style); err != nil {
		return err
	}
	return k.SetStringValue("TileWallpaper", tile)
}

func applyWallpaper(path string) error {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SystemParametersInfoW")
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ret, _, callErr := proc.Call(
		uintptr(spiSetDeskWallpaper),
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(spifUpdateIniFile|spifSendChange),
	)
	if ret == 0 {
		return fmt.Errorf("SystemParametersInfoW returned 0: %v", callErr)
	}
	return nil
}

func readDWORD(k registry.Key, name string, fallback int) int {
	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		return fallback
	}
	return int(v)
}

func writeDWORD(k registry.Key, name string, value int) {
	if value < 0 {
		return
	}
	k.SetDWordValue(name, uint32(value))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
