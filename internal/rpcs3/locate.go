package rpcs3

import (
	"os"
	"path/filepath"
	"runtime"
)

// DefaultGamesYMLPaths returns the candidate locations of RPCS3's games.yml
// for the current OS, in order of likelihood. The returned paths are not
// guaranteed to exist — callers should Stat each one.
func DefaultGamesYMLPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	switch runtime.GOOS {
	case "linux":
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		return []string{filepath.Join(configHome, "rpcs3", "games.yml")}

	case "darwin":
		return []string{
			filepath.Join(home, "Library", "Application Support", "rpcs3", "games.yml"),
		}

	case "windows":
		// RPCS3 on Windows is usually installed portably; games.yml lives in
		// <rpcs3-dir>/config/games.yml. We can't reliably guess the install
		// directory, so we list a few common spots and rely on the user to
		// override via config when none match.
		out := make([]string, 0, 4)
		if appData := os.Getenv("APPDATA"); appData != "" {
			out = append(out, filepath.Join(appData, "rpcs3", "config", "games.yml"))
		}
		out = append(out,
			filepath.Join(home, "Documents", "RPCS3", "config", "games.yml"),
			filepath.Join(home, "RPCS3", "config", "games.yml"),
			`C:\RPCS3\config\games.yml`,
		)
		return out
	}
	return nil
}

// FindGamesYML returns the first existing path from DefaultGamesYMLPaths,
// or empty string if none exists.
func FindGamesYML() string {
	for _, p := range DefaultGamesYMLPaths() {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}
