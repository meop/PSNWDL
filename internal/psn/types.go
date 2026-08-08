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

// emptyTitle builds a Title with no updates. Sony's title-update endpoints
// use an empty/204/404 response to mean "no updates published" for a valid
// title ID, not an error — every console's Lookup* treats it this way.
func emptyTitle(titleID string) *Title {
	return &Title{ID: titleID, Updates: []Update{}}
}
