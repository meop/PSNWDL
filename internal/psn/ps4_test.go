package psn

import (
	"errors"
	"strings"
	"testing"
)

// validPS4VerXML is a synthetic fixture mirroring the PS4 titlepatch XML
// shape.  The PS4 XML omits <paramsfo> and uses manifest_url instead of url
// for the download path; ps4_system_ver names the required system firmware.
const validPS4VerXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<titlepatch status="OK" titleid="CUSA00001">
  <tag name="00_00_00">
    <package version="01.02" size="204800" sha1sum="aabbccddaabbccddaabbccddaabbccddaabbccdd" manifest_url="https://example.com/manifest1.json" ps4_system_ver="09.00" content_id="EP0001-CUSA00001_00-GAME000000000001"/>
    <package version="01.03" size="409600" sha1sum="11223344112233441122334411223344112233dd" url="https://example.com/CUSA00001-v01.03.pkg" ps4_system_ver="09.00"/>
  </tag>
</titlepatch>`

func TestParsePS4VerXML_Valid(t *testing.T) {
	got, err := parsePS4VerXML([]byte(validPS4VerXML), "CUSA00001")
	if err != nil {
		t.Fatalf("parsePS4VerXML: %v", err)
	}
	if got.ID != "CUSA00001" {
		t.Errorf("ID = %q, want CUSA00001", got.ID)
	}
	if len(got.Updates) != 2 {
		t.Fatalf("Updates len = %d, want 2", len(got.Updates))
	}
	if got.Updates[0].Version != "01.02" {
		t.Errorf("Updates[0].Version = %q, want 01.02", got.Updates[0].Version)
	}
	if got.Updates[0].Size != 204800 {
		t.Errorf("Updates[0].Size = %d, want 204800", got.Updates[0].Size)
	}
	// First package has manifest_url only — should be surfaced as URL.
	if got.Updates[0].URL != "https://example.com/manifest1.json" {
		t.Errorf("Updates[0].URL = %q, want manifest url", got.Updates[0].URL)
	}
	// Second package has a direct url attribute — should be preferred.
	if got.Updates[1].URL != "https://example.com/CUSA00001-v01.03.pkg" {
		t.Errorf("Updates[1].URL = %q, want direct pkg url", got.Updates[1].URL)
	}
	if got.Updates[0].SystemVersion != "09.00" {
		t.Errorf("Updates[0].SystemVersion = %q, want 09.00", got.Updates[0].SystemVersion)
	}
}

func TestParsePS4VerXML_TitleMismatch(t *testing.T) {
	_, err := parsePS4VerXML([]byte(validPS4VerXML), "CUSA99999")
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected title mismatch error, got %v", err)
	}
}

func TestParsePS4VerXML_Empty(t *testing.T) {
	_, err := parsePS4VerXML(nil, "CUSA00001")
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestParsePS4VerXML_Malformed(t *testing.T) {
	_, err := parsePS4VerXML([]byte("<not-xml"), "CUSA00001")
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestParsePS4Manifest(t *testing.T) {
	const body = `{
		"pieces": [
			{
				"url": "https://example.com/piece0.pkg",
				"hashValue": "00112233445566778899aabbccddeeff00112233",
				"fileSize": 123456
			}
		]
	}`
	pieces, err := parsePS4Manifest([]byte(body))
	if err != nil {
		t.Fatalf("parsePS4Manifest: %v", err)
	}
	if len(pieces) != 1 {
		t.Fatalf("pieces len = %d, want 1", len(pieces))
	}
	if pieces[0].URL != "https://example.com/piece0.pkg" {
		t.Errorf("URL = %q", pieces[0].URL)
	}
	if pieces[0].HashValue != "00112233445566778899aabbccddeeff00112233" {
		t.Errorf("HashValue = %q", pieces[0].HashValue)
	}
	if pieces[0].FileSize != 123456 {
		t.Errorf("FileSize = %d, want 123456", pieces[0].FileSize)
	}
}

// TestPS4HMAC verifies that the HMAC-SHA256 computation for a known title ID
// produces the exact digest PySN would generate.
//
// Expected value confirmed by running PySN's logic (hmac.new(key, b"np_CUSA00001", sha256).hexdigest()):
const wantPS4HMAC = "1123f23c1f00810a5e43fcb409ada7823bc5ad21b357817e314b6c4832cf6f9f"

func TestPS4HMAC_KnownValue(t *testing.T) {
	got := ps4HMAC("CUSA00001")
	if got != wantPS4HMAC {
		t.Errorf("ps4HMAC(CUSA00001) = %q, want %q", got, wantPS4HMAC)
	}
}
