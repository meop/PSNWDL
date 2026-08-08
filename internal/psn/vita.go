package psn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
)

// vitaHMACKey is the HMAC-SHA256 key Sony uses to authenticate PSVita
// title-update URL paths. Extracted verbatim from PySN.py lines 605-611.
var vitaHMACKey, _ = hex.DecodeString("E5E278AA1EE34082A088279C83F9BBC806821C52F2AB5D2B4ABD995450355114")

const vitaVerXMLEndpoint = "https://gs-sec.ww.np.dl.playstation.net/pl/np/%s/%s/%s-ver.xml"

// vitaTitleIDBytes returns the bytes Sony feeds into HMAC-SHA256 for a Vita
// title: the literal ASCII "np_" prefix followed by the title ID.
func vitaTitleIDBytes(tid string) []byte {
	return []byte("np_" + tid)
}

// vitaHMAC returns the lowercase hex HMAC-SHA256 URL token for the given
// PSVita title ID.
func vitaHMAC(tid string) string {
	mac := hmac.New(sha256.New, vitaHMACKey)
	mac.Write(vitaTitleIDBytes(tid))
	return hex.EncodeToString(mac.Sum(nil))
}

// LookupVita fetches and parses the PSVita ver.xml for the given title ID.
// The endpoint URL contains an HMAC-SHA256 digest that Sony uses to gate
// access; the key is embedded in this package.
func (c *Client) LookupVita(ctx context.Context, tid string) (*Title, error) {
	if err := ValidateTitleID(tid); err != nil {
		return nil, err
	}

	c.activity.Infof("psn", "Resolving Vita title %s", tid)
	digest := vitaHMAC(tid)
	url := fmt.Sprintf(vitaVerXMLEndpoint, tid, digest, tid)
	c.activity.Infof("psn", "GET %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.activity.Errorf("psn", "build request: %v", err)
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.activity.Errorf("psn", "fetch vita ver.xml: %v", err)
		return nil, fmt.Errorf("fetch vita ver.xml: %w", err)
	}
	defer resp.Body.Close()

	c.activity.Infof("psn", "Response status: %d", resp.StatusCode)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		c.activity.Infof("psn", "No updates published for %s", tid)
		return emptyTitle(tid), nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vita ver.xml: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.activity.Errorf("psn", "read vita ver.xml: %v", err)
		return nil, fmt.Errorf("read vita ver.xml: %w", err)
	}

	title, err := parseVitaVerXML(body, tid)
	if err != nil {
		c.activity.Errorf("psn", "parse vita ver.xml: %v", err)
		return nil, err
	}

	c.activity.Infof("psn", "Parsed %d update(s) for %s", len(title.Updates), tid)
	return title, nil
}

// --- PSVita XML shapes ---
//
// The PSVita titlepatch XML uses the same outer structure as PS3 but the
// <package> element carries url, sha1sum, size, and version as direct
// attributes (no <paramsfo> child for the title name).  The system-version
// attribute is named "psp2_system_ver".

type vitaTitlePatch struct {
	XMLName xml.Name     `xml:"titlepatch"`
	Status  string       `xml:"status,attr"`
	TitleID string       `xml:"titleid,attr"`
	Tag     vitaPatchTag `xml:"tag"`
}

type vitaPatchTag struct {
	Packages []vitaPackage `xml:"package"`
}

type vitaPackage struct {
	Version       string `xml:"version,attr"`
	Size          int64  `xml:"size,attr"`
	SHA1Sum       string `xml:"sha1sum,attr"`
	URL           string `xml:"url,attr"`
	SystemVersion string `xml:"psp2_system_ver,attr"`
}

func parseVitaVerXML(body []byte, expectedTID string) (*Title, error) {
	if len(body) == 0 {
		return emptyTitle(expectedTID), nil
	}

	var tp vitaTitlePatch
	if err := xml.Unmarshal(body, &tp); err != nil {
		return nil, fmt.Errorf("parse vita ver.xml: %w", err)
	}

	if tp.TitleID != "" && tp.TitleID != expectedTID {
		return nil, fmt.Errorf("vita ver.xml title mismatch: got %q, want %q", tp.TitleID, expectedTID)
	}

	t := &Title{
		ID:      expectedTID,
		Updates: make([]Update, 0, len(tp.Tag.Packages)),
	}

	for _, p := range tp.Tag.Packages {
		t.Updates = append(t.Updates, Update{
			Version:       p.Version,
			Size:          p.Size,
			SHA1Sum:       p.SHA1Sum,
			URL:           p.URL,
			SystemVersion: p.SystemVersion,
		})
	}

	return t, nil
}
