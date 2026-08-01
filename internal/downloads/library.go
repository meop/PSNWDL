package downloads

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"PSNWDL/internal/config"
)

type File struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

type Title struct {
	Mode          string `json:"mode"`
	TitleID       string `json:"title_id"`
	Locale        string `json:"locale,omitempty"`
	Path          string `json:"path"`
	FileCount     int    `json:"file_count"`
	TotalSize     int64  `json:"total_size"`
	LatestVersion string `json:"latest_version,omitempty"`
	Files         []File `json:"files"`
}

func Scan(root string) ([]Title, error) {
	if root == "" {
		var err error
		root, err = config.DefaultLibraryDir()
		if err != nil {
			return nil, err
		}
	}

	titles := []Title{}
	for _, mode := range []string{"ps3", "ps4", "ps5", "psvita"} {
		firmwareDir, err := config.FirmwareDirForRoot(root, mode)
		if err != nil {
			return nil, err
		}
		localeEntries, err := os.ReadDir(firmwareDir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", firmwareDir, err)
		}
		for _, localeEntry := range localeEntries {
			if !localeEntry.IsDir() {
				continue
			}
			firmware, scanErr := scanTitle(mode, filepath.Join(firmwareDir, localeEntry.Name()), "firmware")
			if scanErr != nil {
				return nil, scanErr
			}
			firmware.Locale = localeEntry.Name()
			if firmware.FileCount > 0 {
				titles = append(titles, firmware)
			}
		}

		titleDir, err := config.TitleDirForRoot(root, mode)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(titleDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", titleDir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			title, err := scanTitle(mode, filepath.Join(titleDir, entry.Name()), entry.Name())
			if err != nil {
				return nil, err
			}
			if title.FileCount > 0 {
				titles = append(titles, title)
			}
		}
	}

	sort.Slice(titles, func(i, j int) bool {
		if titles[i].Mode != titles[j].Mode {
			return titles[i].Mode < titles[j].Mode
		}
		if titles[i].TitleID != titles[j].TitleID {
			return titles[i].TitleID < titles[j].TitleID
		}
		return titles[i].Locale < titles[j].Locale
	})
	return titles, nil
}

func scanTitle(mode, dir, titleID string) (Title, error) {
	title := Title{
		Mode:    mode,
		TitleID: titleID,
		Path:    dir,
	}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		file := File{
			Path:    path,
			Name:    d.Name(),
			Size:    info.Size(),
			Version: versionFromName(titleID, d.Name()),
			Kind:    kindFromName(d.Name()),
		}
		title.FileCount++
		title.TotalSize += file.Size
		if compareVersion(file.Version, title.LatestVersion) > 0 {
			title.LatestVersion = file.Version
		}
		title.Files = append(title.Files, file)
		return nil
	})
	if err != nil {
		return Title{}, fmt.Errorf("scan %s: %w", dir, err)
	}

	sort.Slice(title.Files, func(i, j int) bool {
		if title.Files[i].Version != title.Files[j].Version {
			return compareVersion(title.Files[i].Version, title.Files[j].Version) > 0
		}
		return title.Files[i].Name < title.Files[j].Name
	})
	return title, nil
}

func Delete(root string, targets []string) error {
	if root == "" {
		var err error
		root, err = config.DefaultLibraryDir()
		if err != nil {
			return err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	for _, target := range targets {
		if target == "" {
			continue
		}
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, absTarget)
		if err != nil {
			return err
		}
		if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
			return fmt.Errorf("refusing to delete outside library: %s", target)
		}
		if err := os.RemoveAll(absTarget); err != nil {
			return fmt.Errorf("delete %s: %w", target, err)
		}
		pruneEmptyParents(filepath.Dir(absTarget), root)
	}
	return nil
}

func pruneEmptyParents(dir, root string) {
	for dir != root {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func versionFromName(titleID, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	prefix := titleID + "_"
	if strings.HasPrefix(base, prefix) {
		return strings.TrimPrefix(base, prefix)
	}
	return ""
}

func kindFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pup":
		return "Firmware"
	case ".pkg":
		return "Title update"
	default:
		return "File"
	}
}

func compareVersion(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return -1
	case b == "":
		return 1
	default:
		return strings.Compare(a, b)
	}
}
