package psn

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// FirmwareEntry describes a single firmware PUP available from Sony's CDN.
type FirmwareEntry struct {
	Locale  string `json:"locale"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size,omitempty"`
	SHA1Sum string `json:"sha1sum,omitempty"`
}

// FirmwareList is the merged, deduplicated result of a firmware lookup across
// all locales for one console.
type FirmwareList struct {
	Console string          `json:"console"` // "ps3" | "ps4" | "ps5" | "psvita"
	Entries []FirmwareEntry `json:"entries"`
}

// firmwareLocales is the set of locale codes Sony supports for firmware
// distribution.  Mirrored from PySN.py's locale_list.
var firmwareLocales = []string{"us", "eu", "jp", "kr", "uk", "mx", "au", "sa", "tw", "ru", "cn", "br"}

// ---- PS3 firmware (TXT format) ----

const ps3FirmwareEndpoint = "https://f%s01.ps3.update.playstation.net/update/ps3/list/%s/ps3-updatelist.txt"

// LookupPS3Firmware fetches PS3 firmware listings from all locales.
func (c *Client) LookupPS3Firmware(ctx context.Context) (*FirmwareList, error) {
	list := &FirmwareList{Console: "ps3"}
	c.activity.Infof("psn", "PS3 firmware: fanning out across %d locales", len(firmwareLocales))

	results := c.fetchFirmwareLocales(ctx, "PS3", func(locale string) ([]FirmwareEntry, error) {
		return c.fetchPS3FirmwareTXT(ctx, fmt.Sprintf(ps3FirmwareEndpoint, locale, locale), locale)
	})
	for _, result := range results {
		if result.err != nil {
			// A locale being unavailable is expected — skip and continue.
			c.activity.Warnf("psn", "PS3 firmware %s: unreachable (%v)", result.locale, result.err)
			continue
		}
		c.activity.Infof("psn", "PS3 firmware %s: %d entr%s", result.locale, len(result.entries), pluralY(len(result.entries)))
		list.Entries = append(list.Entries, result.entries...)
	}
	sortFirmwareEntries(list.Entries)

	c.activity.Infof("psn", "PS3 firmware: %d downloadable entr%s", len(list.Entries), pluralY(len(list.Entries)))
	return list, nil
}

func (c *Client) fetchPS3FirmwareTXT(ctx context.Context, url, locale string) ([]FirmwareEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	entries, err := parsePS3FirmwareTXT(body, locale)
	if err != nil {
		return nil, err
	}
	c.fillFirmwareSizes(ctx, entries)
	return entries, nil
}

// parsePS3FirmwareTXT parses the semicolon-delimited ps3-updatelist.txt.
// The format is a run of key=value tokens separated by semicolons.  Relevant
// tokens (mirrored from PySN.py):
//
//	CompatibleSystemSoftwareVersion=XX.XX or SystemSoftwareVersion=X.XXXX
//	SystemSoftwarePackageUrl=https://… or CDN=http://.../PS3UPDAT.PUP
//
// SHA-1 is not published in this file; size must be resolved by a HEAD/GET.
func parsePS3FirmwareTXT(body []byte, locale string) ([]FirmwareEntry, error) {
	var entries []FirmwareEntry

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Each "record" is a semicolon-separated list of key=value pairs on
		// one logical line (PySN splits on ';' then inspects each token).
		tokens := strings.Split(line, ";")

		var ver, dlURL string
		for _, tok := range tokens {
			tok = strings.TrimSpace(tok)
			if strings.HasPrefix(tok, "CompatibleSystemSoftwareVersion=") {
				ver = strings.TrimPrefix(tok, "CompatibleSystemSoftwareVersion=")
				// Sony stores the version as a 4-digit integer, e.g. "0490"
				// meaning 4.90.  Normalise to "X.XX" display form.
				ver = normalisePS3Version(ver)
			}
			if strings.HasPrefix(tok, "SystemSoftwareVersion=") {
				ver = strings.TrimPrefix(tok, "SystemSoftwareVersion=")
				ver = normalisePS3Version(ver)
			}
			if strings.HasPrefix(tok, "SystemSoftwarePackageUrl=") {
				dlURL = strings.TrimPrefix(tok, "SystemSoftwarePackageUrl=")
			}
			if strings.HasPrefix(tok, "CDN=") {
				url := strings.TrimPrefix(tok, "CDN=")
				if strings.Contains(url, "PS3UPDAT.PUP") {
					dlURL = url
				}
			}
		}

		if ver == "" || dlURL == "" {
			continue
		}
		entries = append(entries, FirmwareEntry{
			Locale:  locale,
			Type:    "Firmware",
			Version: ver,
			URL:     dlURL,
		})
	}

	return entries, scanner.Err()
}

// normalisePS3Version converts Sony's raw 4-digit version token to a
// human-readable "X.XX" string (e.g. "0490" → "4.90", "0355" → "3.55").
// If the token is not in the expected 4-digit form it is returned as-is.
func normalisePS3Version(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "-")
	if strings.Count(raw, ".") == 1 {
		parts := strings.Split(raw, ".")
		if len(parts) == 2 && len(parts[1]) > 2 {
			return parts[0] + "." + parts[1][:2]
		}
		return raw
	}
	if len(raw) == 4 {
		// Drop the leading zero and insert a decimal after the first significant digit.
		trimmed := strings.TrimLeft(raw, "0")
		if len(trimmed) == 3 {
			return trimmed[:1] + "." + trimmed[1:]
		}
		if len(trimmed) == 4 {
			return trimmed[:2] + "." + trimmed[2:]
		}
	}
	return raw
}

// ---- PS4 firmware (XML format) ----

const ps4FirmwareEndpoint = "https://f%s01.ps4.update.playstation.net/update/ps4/list/%s/ps4-updatelist.xml"

// LookupPS4Firmware fetches PS4 firmware listings from all locales.
func (c *Client) LookupPS4Firmware(ctx context.Context) (*FirmwareList, error) {
	return c.lookupXMLFirmware(ctx, "ps4", ps4FirmwareEndpoint, "")
}

// LookupPS5Firmware fetches PS5 firmware listings from all locales. The PS5
// endpoint embeds an obfuscation token.
func (c *Client) LookupPS5Firmware(ctx context.Context) (*FirmwareList, error) {
	return c.lookupXMLFirmware(ctx, "ps5", ps5FirmwareEndpoint, ps5FirmwareToken)
}

// LookupVitaFirmware fetches PSVita firmware listings from all locales.
func (c *Client) LookupVitaFirmware(ctx context.Context) (*FirmwareList, error) {
	return c.lookupXMLFirmware(ctx, "psvita", vitaFirmwareEndpoint, "")
}

// lookupXMLFirmware is the shared PS4/PS5/Vita fan-out. Activity console
// narrates the fan-out. token is empty for PS4/Vita; PS5 embeds its
// obfuscation token into the endpoint.
func (c *Client) lookupXMLFirmware(ctx context.Context, console, endpoint, token string) (*FirmwareList, error) {
	list := &FirmwareList{Console: console}
	c.activity.Infof("psn", "%s firmware: fanning out across %d locales", console, len(firmwareLocales))

	results := c.fetchFirmwareLocales(ctx, console, func(locale string) ([]FirmwareEntry, error) {
		var url string
		if token != "" {
			url = fmt.Sprintf(endpoint, locale, token, locale)
		} else {
			url = fmt.Sprintf(endpoint, locale, locale)
		}
		return c.fetchFirmwareXML(ctx, url, locale, console)
	})
	for _, result := range results {
		if result.err != nil {
			c.activity.Warnf("psn", "%s firmware %s: unreachable (%v)", console, result.locale, result.err)
			continue
		}
		c.activity.Infof("psn", "%s firmware %s: %d entr%s", console, result.locale, len(result.entries), pluralY(len(result.entries)))
		list.Entries = append(list.Entries, result.entries...)
	}
	sortFirmwareEntries(list.Entries)

	c.activity.Infof("psn", "%s firmware: %d downloadable entr%s", console, len(list.Entries), pluralY(len(list.Entries)))
	return list, nil
}

type firmwareLocaleResult struct {
	index   int
	locale  string
	entries []FirmwareEntry
	err     error
}

func (c *Client) fetchFirmwareLocales(ctx context.Context, console string, fetch func(locale string) ([]FirmwareEntry, error)) []firmwareLocaleResult {
	results := make([]firmwareLocaleResult, len(firmwareLocales))
	var wg sync.WaitGroup
	for i, locale := range firmwareLocales {
		i, locale := i, locale
		results[i] = firmwareLocaleResult{index: i, locale: locale}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.activity.Infof("psn", "%s firmware %s: fetching", console, locale)
			entries, err := fetch(locale)
			results[i] = firmwareLocaleResult{
				index:   i,
				locale:  locale,
				entries: entries,
				err:     err,
			}
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].index < results[j].index })
	return results
}

func sortFirmwareEntries(entries []FirmwareEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Locale != entries[j].Locale {
			return entries[i].Locale < entries[j].Locale
		}
		if entries[i].Type != entries[j].Type {
			return entries[i].Type < entries[j].Type
		}
		return entries[i].Version > entries[j].Version
	})
}

// pluralY returns "y" for a count of 1, else "ies" — for "entry/entries".
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// ---- PS5 firmware (XML format with obfuscation token) ----

// PS5 firmware URL embeds the fixed obfuscation token (defined in ps5.go).
// Pattern: http://f{locale}01.ps5.update.playstation.net/update/ps5/official/{token}/list/{locale}/updatelist.xml
const ps5FirmwareEndpoint = "http://f%s01.ps5.update.playstation.net/update/ps5/official/%s/list/%s/updatelist.xml"

// ---- PSVita firmware (XML format) ----

const vitaFirmwareEndpoint = "https://f%s01.psp2.update.playstation.net/update/psp2/list/%s/psp2-updatelist.xml"

// ---- Shared XML firmware parser ----

// firmwareUpdateList is the root element shared by PS4, PS5, and PSVita
// firmware XML responses. Sony uses different child shapes:
// PS4/PS5 put downloadable images under <system_pup> and <recovery_pup>;
// Vita uses <version> plus <recovery spkg_type="systemdata|preinst">.
type fwUpdateDataList struct {
	XMLName xml.Name   `xml:"update_data_list"`
	Regions []fwRegion `xml:"region"`
}

type fwRegion struct {
	ID           string          `xml:"id,attr"`
	SystemPUPs   []fwSystemPUP   `xml:"system_pup"`
	RecoveryPUPs []fwRecoveryPUP `xml:"recovery_pup"`
	Versions     []fwVersionNode `xml:"version"`
	Recoveries   []fwRecovery    `xml:"recovery"`
}

type fwUpdateData struct {
	UpdateType string  `xml:"update_type,attr"`
	Image      fwImage `xml:"image"`
}

type fwImage struct {
	Size int64  `xml:"size,attr"`
	URL  string `xml:",chardata"`
}

type fwSystemPUP struct {
	Label             string         `xml:"label,attr"`
	AutoUpdateVersion string         `xml:"auto_update_version,attr"`
	Updates           []fwUpdateData `xml:"update_data"`
}

type fwRecoveryPUP struct {
	Type      string      `xml:"type,attr"`
	SystemPUP fwSystemPUP `xml:"system_pup"`
	Image     fwImage     `xml:"image"`
}

type fwVersionNode struct {
	Label   string         `xml:"label,attr"`
	Updates []fwUpdateData `xml:"update_data"`
}

type fwRecovery struct {
	SPKGType string  `xml:"spkg_type,attr"`
	Image    fwImage `xml:"image"`
}

func (c *Client) fetchFirmwareXML(ctx context.Context, url, locale, console string) ([]FirmwareEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	entries, err := parseFirmwareXML(body, locale)
	if err != nil {
		return nil, err
	}
	c.fillFirmwareSizes(ctx, entries)
	return entries, nil
}

func (c *Client) fillFirmwareSizes(ctx context.Context, entries []FirmwareEntry) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range entries {
		if entries[i].Size > 0 || entries[i].URL == "" {
			continue
		}
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if size := c.contentLength(ctx, entries[i].URL); size > 0 {
				entries[i].Size = size
			}
		}()
	}
	wg.Wait()
}

func (c *Client) contentLength(ctx context.Context, url string) int64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return 0
	}
	return resp.ContentLength
}

// parseFirmwareXML parses a PS4/PS5/PSVita firmware XML document.
// The URL text may have trailing whitespace; it is trimmed.
func parseFirmwareXML(body []byte, locale string) ([]FirmwareEntry, error) {
	if len(body) == 0 {
		return nil, nil
	}

	var list fwUpdateDataList
	if err := xml.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("parse firmware xml: %w", err)
	}

	var entries []FirmwareEntry
	for _, region := range list.Regions {
		entryLocale := locale
		if region.ID != "" {
			entryLocale = region.ID
		}

		for _, pup := range region.SystemPUPs {
			ver := firmwareVersionLabel(pup)
			for _, upd := range pup.Updates {
				if e, ok := firmwareEntry(entryLocale, firmwareType(upd.UpdateType), ver, upd.Image); ok {
					entries = append(entries, e)
				}
			}
		}

		for _, recovery := range region.RecoveryPUPs {
			if e, ok := firmwareEntry(entryLocale, firmwareType(recovery.Type), firmwareVersionLabel(recovery.SystemPUP), recovery.Image); ok {
				entries = append(entries, e)
			}
		}

		for _, version := range region.Versions {
			ver := strings.TrimSpace(version.Label)
			for _, upd := range version.Updates {
				if e, ok := firmwareEntry(entryLocale, firmwareType(upd.UpdateType), ver, upd.Image); ok {
					entries = append(entries, e)
				}
			}
		}

		for _, recovery := range region.Recoveries {
			if e, ok := firmwareEntry(entryLocale, firmwareType(recovery.SPKGType), "", recovery.Image); ok {
				entries = append(entries, e)
			}
		}
	}

	return entries, nil
}

func firmwareEntry(locale, typ, version string, image fwImage) (FirmwareEntry, bool) {
	rawURL := strings.TrimSpace(image.URL)
	if rawURL == "" {
		return FirmwareEntry{}, false
	}
	if typ == "" {
		typ = "Firmware"
	}
	if version == "" {
		version = imageVersionFallback(rawURL)
	}
	return FirmwareEntry{
		Locale:  locale,
		Type:    typ,
		Version: version,
		URL:     rawURL,
		Size:    image.Size,
	}, true
}

func firmwareVersionLabel(pup fwSystemPUP) string {
	label := strings.TrimSpace(pup.Label)
	if label == "" {
		return ""
	}
	// PS5 labels are long build strings. PySN shortens them to the user-facing
	// release token, e.g. 26.04-13.40.00.02-... -> 26.04-13.40.00.
	parts := strings.Split(label, "-")
	if len(parts) >= 2 && strings.Count(parts[1], ".") >= 2 {
		v := strings.Split(parts[1], ".")
		nn := "00"
		if pup.AutoUpdateVersion != "" && pup.AutoUpdateVersion != "00.00" {
			nn = "01"
		}
		return fmt.Sprintf("%s-%s.%s.%s", parts[0], v[0], v[1], nn)
	}
	return label
}

func firmwareType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "full", "system":
		return "Firmware"
	case "default", "recovery":
		return "Recovery"
	case "systemdata":
		return "Fonts"
	case "preinst":
		return "Preinst"
	default:
		return raw
	}
}

func imageVersionFallback(rawURL string) string {
	parts := strings.Split(rawURL, "/")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[len(parts)-2])
	}
	return ""
}
