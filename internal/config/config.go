package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const Version = "1.0"

// Options holds optional migration behaviour settings.
type Options struct {
	Compress         bool   `json:"compress"`
	ConflictStrategy string `json:"conflict_strategy"` // ask, overwrite, skip, rename
	PasswordMode     string `json:"password_mode"`     // skip, assisted, experimental
}

// Config is the top-level migration configuration.
type Config struct {
	Version   string   `json:"version"`
	Hostname  string   `json:"hostname"`
	Date      string   `json:"date"`
	OSVersion string   `json:"os_version"`
	OSBuild   string   `json:"os_build"`
	Users     []string `json:"users"`
	Mode      string   `json:"mode"`    // backup or restore
	Profile   string   `json:"profile"` // simple or advanced
	Jobs      []string `json:"jobs"`
	Options   Options  `json:"options"`
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	hostname, _ := os.Hostname()
	return Config{
		Version:  Version,
		Hostname: hostname,
		Date:     time.Now().Format("2006-01-02"),
		Mode:     "backup",
		Profile:  "simple",
		Jobs:     []string{"userdata", "browsers", "email", "wifi"},
		Options: Options{
			Compress:         false,
			ConflictStrategy: "ask",
			PasswordMode:     "skip",
		},
	}
}

// Load reads and decodes a Config from path.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

// Save encodes cfg as indented JSON and writes it to dir/config.json.
func Save(cfg Config, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}
