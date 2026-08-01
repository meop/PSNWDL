package rpcs3

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Entry is a single game registered in RPCS3's games.yml.
type Entry struct {
	TitleID    string `json:"title_id"`
	InstallDir string `json:"install_dir"`
}

// ParseGamesYML reads RPCS3's games.yml and returns one Entry per registered
// title, sorted by title ID for stable display.
func ParseGamesYML(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read games.yml: %w", err)
	}
	return parseGamesYMLBytes(data)
}

func parseGamesYMLBytes(data []byte) ([]Entry, error) {
	if len(data) == 0 {
		return []Entry{}, nil
	}

	raw := map[string]string{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse games.yml: %w", err)
	}

	out := make([]Entry, 0, len(raw))
	for tid, dir := range raw {
		out = append(out, Entry{TitleID: tid, InstallDir: dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TitleID < out[j].TitleID })
	return out, nil
}
