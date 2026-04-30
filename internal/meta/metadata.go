package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// JobMeta holds per-job statistics recorded during a migration run.
type JobMeta struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // success, warning, error, skipped
	SizeBytes  int64  `json:"size_bytes"`
	FilesCount int    `json:"files_count"`
	Warnings   int    `json:"warnings"`
	Errors     int    `json:"errors"`
	Duration   string `json:"duration"`
}

// Metadata describes a complete migration archive.
type Metadata struct {
	Version   string    `json:"version"`
	Hostname  string    `json:"hostname"`
	Date      string    `json:"date"`
	OSVersion string    `json:"os_version"`
	OSBuild   string    `json:"os_build"`
	Users     []string  `json:"users"`
	Jobs      []JobMeta `json:"jobs"`
	TotalSize int64     `json:"total_size_bytes"`
	Duration  string    `json:"duration"`
	Profile   string    `json:"profile"`
	Cancelled bool      `json:"cancelled,omitempty"`
}

// New constructs a Metadata with the current date and the provided system info.
func New(hostname, osVersion, osBuild string) Metadata {
	return Metadata{
		Version:   "1.0",
		Hostname:  hostname,
		Date:      time.Now().Format("2006-01-02"),
		OSVersion: osVersion,
		OSBuild:   osBuild,
	}
}

// Load reads and decodes Metadata from dir/metadata.json.
func Load(dir string) (Metadata, error) {
	path := filepath.Join(dir, "metadata.json")
	f, err := os.Open(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open metadata: %w", err)
	}
	defer f.Close()
	var m Metadata
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata: %w", err)
	}
	return m, nil
}

// Save encodes m as indented JSON and writes it to dir/metadata.json.
func Save(m Metadata, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create metadata dir: %w", err)
	}
	path := filepath.Join(dir, "metadata.json")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metadata file: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// Exists reports whether dir/metadata.json is present.
func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "metadata.json"))
	return err == nil
}
