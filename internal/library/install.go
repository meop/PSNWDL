package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"PSNWDL/internal/pkg"
)

// InstalledVersion reads the currently installed RPCS3 APP_VER for a title.
// A missing title or PARAM.SFO means the title has no installed update.
func InstalledVersion(hdd0Game, titleID string) (string, error) {
	data, err := os.ReadFile(filepath.Join(hdd0Game, titleID, "PARAM.SFO"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	info, err := pkg.ParseSFO(data)
	if err != nil {
		return "", err
	}
	return info.AppVer, nil
}

func PackageNeedsInstall(packageVersion, installedVersion string) bool {
	if packageVersion == "" {
		return true
	}
	if installedVersion == "" {
		return true
	}
	return strings.Compare(packageVersion, installedVersion) > 0
}
