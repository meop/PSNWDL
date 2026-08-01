package psn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ps4HMACKey is the HMAC-SHA256 key Sony uses to authenticate PS4 title-update
// URL paths. Extracted verbatim from PySN.py lines 605-611.
var ps4HMACKey, _ = hex.DecodeString("AD62E37F905E06BC19593142281C112CEC0E7EC3E97EFDCAEFCDBAAFA6378D84")

const ps4VerXMLEndpoint = "https://gs-sec.ww.np.dl.playstation.net/plo/np/%s/%s/%s-ver.xml"

// ps4TitleID returns the bytes Sony feeds into HMAC-SHA256 for a PS4 title:
// the literal ASCII "np_" prefix followed by the title ID.
func ps4TitleID(tid string) []byte {
	return []byte("np_" + tid)
}

// ps4HMAC returns the lowercase hex HMAC-SHA256 of the PS4 title-update URL
// token for the given title ID.
func ps4HMAC(tid string) string {
	mac := hmac.New(sha256.New, ps4HMACKey)
	mac.Write(ps4TitleID(tid))
	return hex.EncodeToString(mac.Sum(nil))
}

// LookupPS4 fetches and parses the PS4 ver.xml for the given title ID.
// The endpoint URL contains an HMAC-SHA256 digest that Sony uses to gate
// access; the key is embedded in this package.
func (c *Client) LookupPS4(ctx context.Context, tid string) (*Title, error) {
	if err := ValidateTitleID(tid); err != nil {
		return nil, err
	}

	c.activity.Infof("psn", "Resolving PS4 title %s", tid)
	digest := ps4HMAC(tid)
	url := fmt.Sprintf(ps4VerXMLEndpoint, tid, digest, tid)
	c.activity.Infof("psn", "GET %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.activity.Errorf("psn", "build request: %v", err)
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.activity.Errorf("psn", "fetch ps4 ver.xml: %v", err)
		return nil, fmt.Errorf("fetch ps4 ver.xml: %w", err)
	}
	defer resp.Body.Close()

	c.activity.Infof("psn", "Response status: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ps4 ver.xml: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.activity.Errorf("psn", "read ps4 ver.xml: %v", err)
		return nil, fmt.Errorf("read ps4 ver.xml: %w", err)
	}

	title, err := parsePS4VerXML(body, tid)
	if err != nil {
		c.activity.Errorf("psn", "parse ps4 ver.xml: %v", err)
		return nil, err
	}
	if err := c.resolvePS4Manifests(ctx, title); err != nil {
		c.activity.Warnf("psn", "resolve ps4 manifest: %v", err)
	}

	c.activity.Infof("psn", "Parsed %d update(s) for %s", len(title.Updates), tid)
	return title, nil
}

// --- PS4 XML shapes ---
//
// The PS4 titlepatch XML has the same outer <titlepatch> / <tag> / <package>
// skeleton as PS3 but uses "manifest_url" instead of "url" and carries a
// separate "content_id" attribute rather than embedding the title name in a
// <paramsfo> child.  SHA-1 and direct URLs live in a manifest JSON referenced
// by manifest_url; for the purposes of this parser we expose only what is
// available directly in the XML (version, size, system_ver, url/manifest_url).
// LookupPS4 resolves manifest_url into direct downloadable pieces before
// returning to the UI.

type ps4TitlePatch struct {
	XMLName xml.Name    `xml:"titlepatch"`
	Status  string      `xml:"status,attr"`
	TitleID string      `xml:"titleid,attr"`
	Tag     ps4PatchTag `xml:"tag"`
}

type ps4PatchTag struct {
	Packages []ps4Package `xml:"package"`
}

type ps4Package struct {
	Version       string `xml:"version,attr"`
	Size          int64  `xml:"size,attr"`
	SHA1Sum       string `xml:"sha1sum,attr"`
	URL           string `xml:"url,attr"`
	ManifestURL   string `xml:"manifest_url,attr"`
	SystemVersion string `xml:"ps4_system_ver,attr"`
	ContentID     string `xml:"content_id,attr"`
}

func parsePS4VerXML(body []byte, expectedTID string) (*Title, error) {
	if len(body) == 0 {
		return nil, ErrEmptyResponse
	}

	var tp ps4TitlePatch
	if err := xml.Unmarshal(body, &tp); err != nil {
		return nil, fmt.Errorf("parse ps4 ver.xml: %w", err)
	}

	if tp.TitleID != "" && tp.TitleID != expectedTID {
		return nil, fmt.Errorf("ps4 ver.xml title mismatch: got %q, want %q", tp.TitleID, expectedTID)
	}

	t := &Title{
		ID:      expectedTID,
		Updates: make([]Update, 0, len(tp.Tag.Packages)),
	}

	for _, p := range tp.Tag.Packages {
		// PS4 exposes a manifest_url that points to a JSON document listing
		// individual download pieces (with their own URLs and hashes).  We
		// surface manifest_url as the Update.URL so the caller can resolve it;
		// if the XML happens to carry a direct url attribute we prefer that.
		u := p.URL
		if u == "" {
			u = p.ManifestURL
		}
		t.Updates = append(t.Updates, Update{
			Version:       p.Version,
			Size:          p.Size,
			SHA1Sum:       p.SHA1Sum,
			URL:           u,
			SystemVersion: p.SystemVersion,
		})
	}

	return t, nil
}

type ps4Manifest struct {
	Pieces []ps4ManifestPiece `json:"pieces"`
}

type ps4ManifestPiece struct {
	URL       string `json:"url"`
	HashValue string `json:"hashValue"`
	FileSize  int64  `json:"fileSize"`
}

func (c *Client) resolvePS4Manifests(ctx context.Context, title *Title) error {
	var resolved []Update
	var firstErr error
	for _, u := range title.Updates {
		if u.URL == "" || !strings.HasSuffix(u.URL, ".json") {
			resolved = append(resolved, u)
			continue
		}

		c.activity.Infof("psn", "GET PS4 manifest %s", u.URL)
		pieces, err := c.fetchPS4Manifest(ctx, u.URL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// Keep the manifest row visible, but mark it non-downloadable by
			// leaving it as-is. The UI can still show the URL and error.
			resolved = append(resolved, u)
			continue
		}
		c.activity.Infof("psn", "Resolved PS4 manifest into %d piece(s)", len(pieces))
		for i, p := range pieces {
			if p.URL == "" {
				continue
			}
			piece := u
			piece.URL = p.URL
			piece.SHA1Sum = p.HashValue
			piece.Size = p.FileSize
			if len(pieces) > 1 {
				piece.Version = fmt.Sprintf("%s part %d", u.Version, i+1)
			}
			resolved = append(resolved, piece)
		}
	}
	title.Updates = resolved
	return firstErr
}

func (c *Client) fetchPS4Manifest(ctx context.Context, url string) ([]ps4ManifestPiece, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build manifest request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return parsePS4Manifest(body)
}

func parsePS4Manifest(body []byte) ([]ps4ManifestPiece, error) {
	var manifest ps4Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest json: %w", err)
	}
	return manifest.Pieces, nil
}
