package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var libraryModes = []string{"ps3", "ps4", "ps5", "psvita"}

// MigrateLibraryLayout upgrades the v1 <root>/<mode>/updates/<title> layout
// to v2's <root>/<mode>/{firmware,title} layout. The old default root is also
// renamed from ~/.psnwdl/download to ~/.psnwdl/library. Custom roots are kept.
func MigrateLibraryLayout(cfg *Config) (bool, error) {
	if cfg == nil || cfg.SchemaVersion >= SchemaVersion {
		return false, nil
	}

	oldRoot := strings.TrimSpace(cfg.Storage.LibraryDir)
	if oldRoot == "" {
		var err error
		oldRoot, err = legacyDefaultLibraryDir()
		if err != nil {
			return false, fmt.Errorf("resolve legacy library: %w", err)
		}
	}
	newRoot := oldRoot
	legacyRoot, legacyErr := legacyDefaultLibraryDir()
	defaultRoot, defaultErr := DefaultLibraryDir()
	if legacyErr == nil && defaultErr == nil && samePath(oldRoot, legacyRoot) {
		newRoot = defaultRoot
	}

	for _, mode := range libraryModes {
		legacyUpdates := filepath.Join(oldRoot, mode, "updates")
		entries, err := os.ReadDir(legacyUpdates)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("read legacy library %s: %w", legacyUpdates, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			source := filepath.Join(legacyUpdates, entry.Name())
			var destination string
			if strings.EqualFold(entry.Name(), "firmware") {
				destination = filepath.Join(newRoot, mode, "firmware")
			} else {
				destination = filepath.Join(newRoot, mode, "title", entry.Name())
			}
			if err := moveLegacyDirectory(source, destination); err != nil {
				return false, fmt.Errorf("migrate %s: %w", source, err)
			}
		}
		removeIfEmpty(legacyUpdates)
		removeIfEmpty(filepath.Join(oldRoot, mode))
	}

	cfg.Storage.LibraryDir = newRoot
	cfg.SchemaVersion = SchemaVersion
	removeIfEmpty(oldRoot)
	return true, nil
}

func moveLegacyDirectory(source, destination string) error {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.Rename(source, destination)
	} else if err != nil {
		return err
	}

	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourceChild := filepath.Join(source, entry.Name())
		destinationChild := filepath.Join(destination, entry.Name())
		if entry.IsDir() {
			if err := moveLegacyDirectory(sourceChild, destinationChild); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Stat(destinationChild); err == nil {
			return fmt.Errorf("destination already exists: %s", destinationChild)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(sourceChild, destinationChild); err != nil {
			return err
		}
	}
	removeIfEmpty(source)
	return nil
}

func removeIfEmpty(path string) {
	_ = os.Remove(path)
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
