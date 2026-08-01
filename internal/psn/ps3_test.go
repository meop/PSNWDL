package psn

import (
	"errors"
	"strings"
	"testing"
)

const validPS3VerXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<titlepatch status="OK" titleid="BCUS98114">
  <tag name="00_00_00">
    <package version="01.04" size="1024" sha1sum="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" url="http://example.com/u1.pkg" ps3_system_ver="03.55">
      <paramsfo>
        <TITLE>Gran Turismo 5</TITLE>
      </paramsfo>
    </package>
    <package version="01.05" size="2048" sha1sum="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" url="http://example.com/u2.pkg" ps3_system_ver="03.55" drm_type="0">
      <paramsfo>
        <TITLE>Gran Turismo 5</TITLE>
      </paramsfo>
    </package>
  </tag>
</titlepatch>`

func TestValidateTitleID(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"BCUS98114", false},
		{"NPEB00301", false},
		{"BLES01234", false},
		{"bcus98114", true},  // lowercase
		{"BCUS9811", true},   // 4 digits
		{"BCUS981144", true}, // 6 digits
		{"BCU598114", true},  // digit in letter slot
		{"", true},
	}
	for _, tc := range cases {
		err := ValidateTitleID(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateTitleID(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestParsePS3VerXML_Valid(t *testing.T) {
	got, err := parsePS3VerXML([]byte(validPS3VerXML), "BCUS98114")
	if err != nil {
		t.Fatalf("parsePS3VerXML: %v", err)
	}
	if got.ID != "BCUS98114" {
		t.Errorf("ID = %q, want BCUS98114", got.ID)
	}
	if got.Name != "Gran Turismo 5" {
		t.Errorf("Name = %q, want Gran Turismo 5", got.Name)
	}
	if len(got.Updates) != 2 {
		t.Fatalf("Updates len = %d, want 2", len(got.Updates))
	}
	if got.Updates[0].Version != "01.04" || got.Updates[1].Version != "01.05" {
		t.Errorf("versions = [%q, %q], want [01.04, 01.05]",
			got.Updates[0].Version, got.Updates[1].Version)
	}
	if got.Updates[0].Size != 1024 || got.Updates[1].Size != 2048 {
		t.Errorf("sizes = [%d, %d], want [1024, 2048]",
			got.Updates[0].Size, got.Updates[1].Size)
	}
	if got.Updates[1].DRMType != "0" {
		t.Errorf("Updates[1].DRMType = %q, want 0", got.Updates[1].DRMType)
	}
	if got.Updates[0].SystemVersion != "03.55" {
		t.Errorf("SystemVersion = %q, want 03.55", got.Updates[0].SystemVersion)
	}
}

func TestParsePS3VerXML_DRMFreeRows(t *testing.T) {
	xml := `<?xml version="1.0"?>
<titlepatch status="OK" titleid="NPEA00001">
  <tag name="00_00_00">
    <package version="01.00" size="1024" sha1sum="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" url="http://example.com/regular.pkg" ps3_system_ver="03.55">
      <paramsfo><TITLE>Sample Game</TITLE></paramsfo>
      <url size="512" sha1sum="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" url="http://example.com/drm-free.pkg"/>
    </package>
  </tag>
</titlepatch>`

	regular, err := parsePS3VerXMLWithOptions([]byte(xml), "NPEA00001", false)
	if err != nil {
		t.Fatalf("parse regular: %v", err)
	}
	if len(regular.Updates) != 1 {
		t.Fatalf("regular updates len = %d, want 1", len(regular.Updates))
	}

	withDRMFree, err := parsePS3VerXMLWithOptions([]byte(xml), "NPEA00001", true)
	if err != nil {
		t.Fatalf("parse drm-free: %v", err)
	}
	if len(withDRMFree.Updates) != 2 {
		t.Fatalf("updates len = %d, want 2", len(withDRMFree.Updates))
	}
	got := withDRMFree.Updates[1]
	if got.DRMType != "drm_free" {
		t.Errorf("DRMType = %q, want drm_free", got.DRMType)
	}
	if got.Version != "01.00" {
		t.Errorf("Version = %q, want package version fallback", got.Version)
	}
	if got.URL != "http://example.com/drm-free.pkg" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Size != 512 {
		t.Errorf("Size = %d, want 512", got.Size)
	}
}

func TestParsePS3VerXML_TitleMismatch(t *testing.T) {
	_, err := parsePS3VerXML([]byte(validPS3VerXML), "BLES01234")
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected title mismatch error, got %v", err)
	}
}

func TestParsePS3VerXML_Empty(t *testing.T) {
	_, err := parsePS3VerXML(nil, "BCUS98114")
	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestParsePS3VerXML_Malformed(t *testing.T) {
	_, err := parsePS3VerXML([]byte("<not-xml"), "BCUS98114")
	if err == nil {
		t.Error("expected parse error, got nil")
	}
}

func TestParsePS3VerXML_NoTitle(t *testing.T) {
	xml := `<?xml version="1.0"?>
<titlepatch status="OK" titleid="BCUS98114">
  <tag name="00_00_00">
    <package version="01.00" size="512" sha1sum="cccccccccccccccccccccccccccccccccccccccc" url="http://example.com/u.pkg"/>
  </tag>
</titlepatch>`
	got, err := parsePS3VerXML([]byte(xml), "BCUS98114")
	if err != nil {
		t.Fatalf("parsePS3VerXML: %v", err)
	}
	if got.Name != "" {
		t.Errorf("Name = %q, want empty", got.Name)
	}
	if len(got.Updates) != 1 {
		t.Fatalf("Updates len = %d, want 1", len(got.Updates))
	}
}
