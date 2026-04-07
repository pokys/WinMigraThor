package cleanup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const tempDir = `migrator`

// Item represents a file or directory found during cleanup scan.
type Item struct {
	Path        string
	Description string
	SizeBytes   int64
	IsDir       bool
}

// Manifest tracks files created by the tool.
type Manifest struct {
	Items []ManifestEntry `json:"items"`
}

// ManifestEntry records a single path that the tool created.
type ManifestEntry struct {
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	Purpose   string    `json:"purpose"`
}

// TempDir returns the tool's temp directory inside the system temp folder.
func TempDir() string {
	tmp := os.Getenv("TEMP")
	if tmp == "" {
		tmp = os.TempDir()
	}
	return filepath.Join(tmp, tempDir)
}

// ManifestPath returns the path to the manifest file.
func ManifestPath() string {
	return filepath.Join(TempDir(), "manifest.json")
}

// AddToManifest records a file in the manifest.
func AddToManifest(path, purpose string) error {
	m := loadManifest()
	m.Items = append(m.Items, ManifestEntry{
		Path:      path,
		CreatedAt: time.Now(),
		Purpose:   purpose,
	})
	return saveManifest(m)
}

func loadManifest() Manifest {
	var m Manifest
	f, err := os.Open(ManifestPath())
	if err != nil {
		return m
	}
	defer f.Close()
	json.NewDecoder(f).Decode(&m) //nolint:errcheck // best-effort load
	return m
}

func saveManifest(m Manifest) error {
	if err := os.MkdirAll(TempDir(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(ManifestPath())
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// Scan finds all temporary files created by the tool.
func Scan() ([]Item, error) {
	var items []Item

	// Scan tool temp dir
	td := TempDir()
	if _, err := os.Stat(td); err == nil {
		size := dirSize(td)
		items = append(items, Item{
			Path:        td,
			Description: "Temp export files",
			SizeBytes:   size,
			IsDir:       true,
		})
	}

	// Scan for password CSV files in the user's Downloads folder.
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	knownCSVNames := []string{
		"passwords.csv",
		"chrome_passwords.csv",
		"edge_passwords.csv",
		"firefox_passwords.csv",
		"Chrome Passwords.csv",
	}
	downloads := filepath.Join(home, "Downloads")
	for _, name := range knownCSVNames {
		path := filepath.Join(downloads, name)
		if info, err := os.Stat(path); err == nil {
			items = append(items, Item{
				Path:        path,
				Description: "Password CSV file",
				SizeBytes:   info.Size(),
				IsDir:       false,
			})
		}
	}

	// Scan for stale lock files
	lockPath := filepath.Join(td, ".migrator.lock")
	if info, err := os.Stat(lockPath); err == nil {
		items = append(items, Item{
			Path:        lockPath,
			Description: "Stale lock file",
			SizeBytes:   info.Size(),
			IsDir:       false,
		})
	}

	return items, nil
}

// Delete removes a list of items, collecting all errors before returning.
func Delete(items []Item) error {
	var errs []string
	for _, item := range items {
		var err error
		if item.IsDir {
			err = os.RemoveAll(item.Path)
		} else {
			err = os.Remove(item.Path)
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", item.Path, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}
	return nil
}

func dirSize(path string) int64 {
	var size int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error { //nolint:errcheck
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
