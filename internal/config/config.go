package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const SchemaVersion = 2

type Config struct {
	SchemaVersion int     `toml:"schema_version" json:"schema_version"`
	RPCS3         RPCS3   `toml:"rpcs3"          json:"rpcs3"`
	Storage       Storage `toml:"storage"        json:"storage"`
	Network       Network `toml:"network"        json:"network"`
	UI            UI      `toml:"ui"             json:"ui"`

	// HomeDir is not stored in config.toml; it is populated in-memory at load
	// so the UI can show resolved absolute paths (e.g. C:\Users\you\.psnwdl\…)
	// instead of the literal ~/ the frontend used to fall back to.
	HomeDir string `toml:"-" json:"home_dir"`
}

type RPCS3 struct {
	GamesYML string `toml:"games_yml" json:"games_yml"`
	HDD0Game string `toml:"hdd0_game" json:"hdd0_game"`
}

type Storage struct {
	LibraryDir string `toml:"library_dir" json:"library_dir"`
}

type Network struct {
	MaxConcurrentDownloads int  `toml:"max_concurrent_downloads" json:"max_concurrent_downloads"`
	VerifyTLS              bool `toml:"verify_tls"               json:"verify_tls"`
	RequestTimeoutSeconds  int  `toml:"request_timeout_seconds"  json:"request_timeout_seconds"`
	RetryCount             int  `toml:"retry_count"              json:"retry_count"`
}

type UI struct {
	Theme           string `toml:"theme"            json:"theme"`
	DefaultMode     string `toml:"default_mode"     json:"default_mode"`
	DefaultDownload string `toml:"default_download" json:"default_download"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	libraryDir := ""
	if defaultLibraryDir, err := DefaultLibraryDir(); err == nil {
		libraryDir = defaultLibraryDir
	}
	return &Config{
		SchemaVersion: SchemaVersion,
		Storage: Storage{
			LibraryDir: libraryDir,
		},
		Network: Network{
			MaxConcurrentDownloads: 5,
			VerifyTLS:              false,
			RequestTimeoutSeconds:  15,
			RetryCount:             3,
		},
		UI: UI{
			Theme:           "system",
			DefaultMode:     "ps3",
			DefaultDownload: "firmware",
		},
		HomeDir: home,
	}
}

// Load reads the config file at path. If it does not exist, defaults are
// returned and written to disk, so subsequent calls find a real file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			cfg := Default()
			if err := Save(path, cfg); err != nil {
				return nil, fmt.Errorf("write default config: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if !bytes.Contains(data, []byte("retry_count")) {
		cfg.Network.RetryCount = Default().Network.RetryCount
	}
	if !bytes.Contains(data, []byte("default_download")) {
		cfg.UI.DefaultDownload = Default().UI.DefaultDownload
	}
	if cfg.HomeDir == "" {
		cfg.HomeDir, _ = os.UserHomeDir()
	}
	return cfg, nil
}

// Save writes the config atomically: marshal → temp file → rename.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp config: %w", err)
	}
	return nil
}
