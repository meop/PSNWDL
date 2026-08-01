package config

import (
	"os"
	"path/filepath"
)

const baseDirName = ".psnwdl"

// Home returns the PSNWDL base directory: <user-home>/.psnwdl.
// On Windows this resolves under %USERPROFILE%; on *nix under $HOME.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, baseDirName), nil
}

func DefaultLibraryDir() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "library"), nil
}

// ConfigPath returns the config file path, honoring $PSNETDL_CONFIG if set.
func ConfigPath() (string, error) {
	if override := os.Getenv("PSNETDL_CONFIG"); override != "" {
		return override, nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "config.toml"), nil
}

func legacyDefaultLibraryDir() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "download"), nil
}

func TitleDirForRoot(root, mode string) (string, error) {
	if root == "" {
		var err error
		root, err = DefaultLibraryDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, mode, "title"), nil
}

func FirmwareDirForRoot(root, mode string) (string, error) {
	if root == "" {
		var err error
		root, err = DefaultLibraryDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, mode, "firmware"), nil
}
