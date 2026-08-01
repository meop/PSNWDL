package psn

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
)

const ps3VerXMLEndpoint = "https://a0.ww.np.dl.playstation.net/tpl/np/%s/%s-ver.xml"

var titleIDRe = regexp.MustCompile(`^[A-Z]{4}\d{5}$`)

// ValidateTitleID returns nil for the canonical 4-letter + 5-digit PSN title
// ID shape (e.g. BCUS98114, NPEB00301).
func ValidateTitleID(tid string) error {
	if !titleIDRe.MatchString(tid) {
		return fmt.Errorf("invalid title id %q: expected 4 uppercase letters + 5 digits", tid)
	}
	return nil
}

// Client talks to Sony's title-update endpoints.
type Client struct {
	http     *http.Client
	activity *activity.Sink
}

// NewClient builds an HTTP client honoring the given network config.
// When VerifyTLS is false (default for Sony endpoints), certificate
// validation is skipped — Sony's title-update hosts present certs the Go
// default roots reject.
func NewClient(net config.Network, act *activity.Sink) *Client {
	timeout := time.Duration(net.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !net.VerifyTLS}, // #nosec G402 — PSN endpoints require this
	}

	return &Client{
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		activity: act,
	}
}

func (c *Client) LookupPS3(ctx context.Context, tid string) (*Title, error) {
	return c.LookupPS3WithDRMFree(ctx, tid, false)
}

// LookupPS3WithDRMFree fetches and parses the PS3 ver.xml for the given title
// ID. When includeDRMFree is true, PySN-style nested <url> entries are
// surfaced as separate DRM-free update rows.
func (c *Client) LookupPS3WithDRMFree(ctx context.Context, tid string, includeDRMFree bool) (*Title, error) {
	if err := ValidateTitleID(tid); err != nil {
		return nil, err
	}

	c.activity.Infof("psn", "Resolving PS3 title %s", tid)
	url := fmt.Sprintf(ps3VerXMLEndpoint, tid, tid)
	c.activity.Infof("psn", "GET %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.activity.Errorf("psn", "build request: %v", err)
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.activity.Errorf("psn", "fetch ver.xml: %v", err)
		return nil, fmt.Errorf("fetch ver.xml: %w", err)
	}
	defer resp.Body.Close()

	c.activity.Infof("psn", "Response status: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ver.xml: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.activity.Errorf("psn", "read ver.xml: %v", err)
		return nil, fmt.Errorf("read ver.xml: %w", err)
	}

	title, err := parsePS3VerXMLWithOptions(body, tid, includeDRMFree)
	if err != nil {
		c.activity.Errorf("psn", "parse ver.xml: %v", err)
		return nil, err
	}

	c.activity.Infof("psn", "Parsed %d update(s) for %s", len(title.Updates), tid)
	return title, nil
}

// --- XML shapes (internal) ---

type ps3TitlePatch struct {
	XMLName xml.Name    `xml:"titlepatch"`
	Status  string      `xml:"status,attr"`
	TitleID string      `xml:"titleid,attr"`
	Tag     ps3PatchTag `xml:"tag"`
}

type ps3PatchTag struct {
	Packages []ps3Package `xml:"package"`
}

type ps3Package struct {
	Version       string       `xml:"version,attr"`
	Size          int64        `xml:"size,attr"`
	SHA1Sum       string       `xml:"sha1sum,attr"`
	URL           string       `xml:"url,attr"`
	SystemVersion string       `xml:"ps3_system_ver,attr"`
	DRMType       string       `xml:"drm_type,attr"`
	ParamSFO      ps3ParamSFO  `xml:"paramsfo"`
	URLs          []ps3URLNode `xml:"url"`
}

type ps3ParamSFO struct {
	Title string `xml:"TITLE"`
}

type ps3URLNode struct {
	Version string `xml:"version,attr"`
	Size    int64  `xml:"size,attr"`
	SHA1Sum string `xml:"sha1sum,attr"`
	URL     string `xml:"url,attr"`
}

// ErrEmptyResponse is returned when Sony's endpoint returns 200 but no
// titlepatch body — typically meaning the title has no published updates.
var ErrEmptyResponse = errors.New("empty ver.xml response")

func parsePS3VerXML(body []byte, expectedTID string) (*Title, error) {
	return parsePS3VerXMLWithOptions(body, expectedTID, false)
}

func parsePS3VerXMLWithOptions(body []byte, expectedTID string, includeDRMFree bool) (*Title, error) {
	if len(body) == 0 {
		return nil, ErrEmptyResponse
	}

	var tp ps3TitlePatch
	if err := xml.Unmarshal(body, &tp); err != nil {
		return nil, fmt.Errorf("parse ver.xml: %w", err)
	}

	if tp.TitleID != "" && tp.TitleID != expectedTID {
		return nil, fmt.Errorf("ver.xml title mismatch: got %q, want %q", tp.TitleID, expectedTID)
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
			DRMType:       p.DRMType,
		})
		if includeDRMFree {
			for _, u := range p.URLs {
				if u.URL == "" {
					continue
				}
				version := u.Version
				if version == "" {
					version = p.Version
				}
				t.Updates = append(t.Updates, Update{
					Version:       version,
					Size:          u.Size,
					SHA1Sum:       u.SHA1Sum,
					URL:           u.URL,
					SystemVersion: p.SystemVersion,
					DRMType:       "drm_free",
				})
			}
		}
		if t.Name == "" && p.ParamSFO.Title != "" {
			t.Name = p.ParamSFO.Title
		}
	}

	return t, nil
}
