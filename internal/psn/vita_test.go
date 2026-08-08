package psn

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"PSNWDL/internal/activity"
)

// validVitaVerXML is a synthetic fixture mirroring the PSVita titlepatch XML
// shape.  Vita uses psp2_system_ver and has no <paramsfo> child.
const validVitaVerXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<titlepatch status="OK" titleid="PCSA00001">
  <tag name="00_00_00">
    <package version="01.10" size="65536" sha1sum="aabbccddaabbccddaabbccddaabbccddaabbccdd" url="https://example.com/PCSA00001-v01.10.pkg" psp2_system_ver="03.60"/>
    <package version="01.11" size="131072" sha1sum="11223344112233441122334411223344112233dd" url="https://example.com/PCSA00001-v01.11.pkg" psp2_system_ver="03.60"/>
  </tag>
</titlepatch>`

func TestParseVitaVerXML_Valid(t *testing.T) {
	got, err := parseVitaVerXML([]byte(validVitaVerXML), "PCSA00001")
	if err != nil {
		t.Fatalf("parseVitaVerXML: %v", err)
	}
	if got.ID != "PCSA00001" {
		t.Errorf("ID = %q, want PCSA00001", got.ID)
	}
	if len(got.Updates) != 2 {
		t.Fatalf("Updates len = %d, want 2", len(got.Updates))
	}
	if got.Updates[0].Version != "01.10" {
		t.Errorf("Updates[0].Version = %q, want 01.10", got.Updates[0].Version)
	}
	if got.Updates[0].Size != 65536 {
		t.Errorf("Updates[0].Size = %d, want 65536", got.Updates[0].Size)
	}
	if got.Updates[0].SHA1Sum != "aabbccddaabbccddaabbccddaabbccddaabbccdd" {
		t.Errorf("Updates[0].SHA1Sum = %q", got.Updates[0].SHA1Sum)
	}
	if got.Updates[0].URL != "https://example.com/PCSA00001-v01.10.pkg" {
		t.Errorf("Updates[0].URL = %q", got.Updates[0].URL)
	}
	if got.Updates[0].SystemVersion != "03.60" {
		t.Errorf("Updates[0].SystemVersion = %q, want 03.60", got.Updates[0].SystemVersion)
	}
}

func TestParseVitaVerXML_TitleMismatch(t *testing.T) {
	_, err := parseVitaVerXML([]byte(validVitaVerXML), "PCSA99999")
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected title mismatch error, got %v", err)
	}
}

func TestParseVitaVerXML_Empty(t *testing.T) {
	got, err := parseVitaVerXML(nil, "PCSA00001")
	if err != nil {
		t.Fatalf("parse empty response: %v", err)
	}
	if got.ID != "PCSA00001" || len(got.Updates) != 0 {
		t.Errorf("empty response = %+v, want title with no updates", got)
	}
}

func TestParseVitaVerXML_Malformed(t *testing.T) {
	_, err := parseVitaVerXML([]byte("<bad-xml"), "PCSA00001")
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

// TestVitaHMAC_KnownValue verifies the HMAC-SHA256 digest for a known Vita
// title ID matches what PySN would produce.
//
// Expected value confirmed by running PySN's logic (hmac.new(key, b"np_PCSA00001", sha256).hexdigest()):
const wantVitaHMAC = "6aa1b35f0e0a7992922d288c0a21f010875f32972994c977dd97cd9878a329e3"

func TestVitaHMAC_KnownValue(t *testing.T) {
	got := vitaHMAC("PCSA00001")
	if got != wantVitaHMAC {
		t.Errorf("vitaHMAC(PCSA00001) = %q, want %q", got, wantVitaHMAC)
	}
}

// TestLookupVitaTreatsEmptyResponsesAsNoUpdates mirrors PS3's equivalent
// test: a title with no published updates must resolve successfully with
// zero Updates, not as an error.
func TestLookupVitaTreatsEmptyResponsesAsNoUpdates(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &Client{
				http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.Method != http.MethodGet || !strings.Contains(req.URL.Path, "PCSA00001-ver.xml") {
						t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
					}
					return &http.Response{
						StatusCode: status,
						Body:       io.NopCloser(bytes.NewReader(nil)),
						Header:     make(http.Header),
					}, nil
				})},
				activity: activity.NewSink(nil),
			}

			title, err := client.LookupVita(context.Background(), "PCSA00001")
			if err != nil {
				t.Fatalf("LookupVita: %v", err)
			}
			if title.ID != "PCSA00001" || len(title.Updates) != 0 {
				t.Fatalf("title = %+v, want no updates", title)
			}
		})
	}
}
