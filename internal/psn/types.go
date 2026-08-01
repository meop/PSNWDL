package psn

// Update describes a single title-update package as advertised by Sony.
type Update struct {
	Version       string `json:"version"`
	Size          int64  `json:"size"`
	SHA1Sum       string `json:"sha1sum"`
	URL           string `json:"url"`
	SystemVersion string `json:"system_version,omitempty"`
	DRMType       string `json:"drm_type,omitempty"`
}

// Title is the resolved lookup result for a single PSN title ID.
type Title struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Updates []Update `json:"updates"`
}
