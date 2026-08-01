package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var libraryModes = []string{"ps3", "ps4", "ps5", "psvita"}

// MigrateLibraryLayout upgrades older library layouts to the current schema:
//   - v1 moves <root>/<mode>/updates/<title> into {firmware,title} branches.
//   - v2 moves flat firmware files into firmware/unknown because their locale
//     cannot be recovered from the old path.
//
// The v1 default root is also renamed from ~/.psnwdl/download to
// ~/.psnwdl/library. Custom roots are kept.
func MigrateLibraryLayout(cfg *Config) (bool, error) {
	if cfg == nil || cfg.SchemaVersion >= SchemaVersion {
		return false, nil
	}

	originalVersion := cfg.SchemaVersion
	root := strings.TrimSpace(cfg.Storage.LibraryDir)
	if originalVersion < 2 {
		if root == "" {
			var err error
			root, err = legacyDefaultLibraryDir()
			if err != nil {
				return false, fmt.Errorf("resolve legacy library: %w", err)
			}
		}
		oldRoot := root
		legacyRoot, legacyErr := legacyDefaultLibraryDir()
		defaultRoot, defaultErr := DefaultLibraryDir()
		if legacyErr == nil && defaultErr == nil && samePath(oldRoot, legacyRoot) {
			root = defaultRoot
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
					destination = filepath.Join(root, mode, "firmware")
				} else {
					destination = filepath.Join(root, mode, "title", entry.Name())
				}
				if err := moveLegacyDirectory(source, destination); err != nil {
					return false, fmt.Errorf("migrate %s: %w", source, err)
				}
			}
			removeIfEmpty(legacyUpdates)
			removeIfEmpty(filepath.Join(oldRoot, mode))
		}
		removeIfEmpty(oldRoot)
	} else if root == "" {
		var err error
		root, err = DefaultLibraryDir()
		if err != nil {
			return false, fmt.Errorf("resolve library: %w", err)
		}
	}

	if originalVersion < 3 {
		if err := migrateFirmwareLocales(root); err != nil {
			return false, err
		}
	}

	cfg.Storage.LibraryDir = root
	cfg.SchemaVersion = SchemaVersion
	return true, nil
}

func migrateFirmwareLocales(root string) error {
	for _, mode := range libraryModes {
		firmwareRoot := filepath.Join(root, mode, "firmware")
		entries, err := os.ReadDir(firmwareRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read firmware library %s: %w", firmwareRoot, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			source := filepath.Join(firmwareRoot, entry.Name())
			destination := filepath.Join(firmwareRoot, "unknown", entry.Name())
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return fmt.Errorf("create firmware locale folder: %w", err)
			}
			if _, err := os.Stat(destination); err == nil {
				return fmt.Errorf("destination already exists: %s", destination)
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(source, destination); err != nil {
				return fmt.Errorf("migrate firmware %s: %w", source, err)
			}
		}
	}
	return nil
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
